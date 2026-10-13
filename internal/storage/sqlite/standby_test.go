package sqlite

import (
	"context"
	"testing"
)

func TestStandbyWaitsForDurableJobsAndWebhookOutbox(t *testing.T) {
	s := openStore(t.Context(), t)
	ctx := context.Background()
	exec := func(query string) {
		t.Helper()

		if _, err := s.db.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	check := func(want bool) {
		t.Helper()

		got, err := s.HasBackgroundWork(ctx)
		if err != nil || got != want {
			t.Fatalf("background work=%v, %v; want %v", got, err, want)
		}
	}
	check(false)
	exec(`INSERT INTO import_jobs(id,importer,source,state,duplicate_policy,total_items,created_at) VALUES('idle-import','sref','fixture','running','reject',1,'2026-01-01T00:00:00Z')`)
	check(true)
	exec(`UPDATE import_jobs SET state='completed'`)
	check(false)
	exec(`INSERT INTO automation_jobs(id,library_id,actor_id,kind,state,revision,created_at,expires_at,record_json) SELECT 'idle-job','default',id,'export','committing',1,unixepoch(),unixepoch()+3600,'{}' FROM users LIMIT 1`)
	check(true)
	exec(`UPDATE automation_jobs SET kind='import', record_json='{"job":{"import_job_id":"idle-import"}}'`)
	check(false)

	for _, state := range []string{"completed-with-errors", "cancelled", "failed", "interrupted"} {
		if _, err := s.db.ExecContext(ctx, `UPDATE import_jobs SET state=?`, state); err != nil {
			t.Fatal(err)
		}

		check(false)
	}

	exec(`UPDATE import_jobs SET state='running'`)
	check(true)
	exec(`UPDATE import_jobs SET state='completed'`)
	exec(`UPDATE automation_jobs SET record_json='{"job":{"import_job_id":"missing"}}'`)
	check(true)
	exec(`UPDATE automation_jobs SET kind='export', record_json='{"job":{"import_job_id":"idle-import"}}'`)
	check(true)
	exec(`UPDATE automation_jobs SET state='completed'`)
	check(false)
	exec(`INSERT INTO domain_events(id,event_type,library_id,occurred_at,envelope_json) VALUES('idle-event','recipe.created','default',unixepoch(),'{}')`)
	check(true)
	exec(`UPDATE event_consumers SET sequence=(SELECT MAX(sequence) FROM domain_events) WHERE id='webhooks'`)
	check(false)
	exec(`INSERT INTO webhook_subscriptions(id,library_id,name,url,event_types,enabled,revision,created_at,updated_at,updated_by,rotated_at,secret_ref,created_sequence) VALUES('idle-hook','default','Hook','https://example.com','[]',1,1,0,0,'',0,'private',0)`)
	exec(`INSERT INTO webhook_deliveries(id,subscription_id,event_id,event_type,state,created_at,cycle_at,next_attempt_at,body) VALUES('idle-delivery','idle-hook','idle-event','recipe.created','retrying',0,0,unixepoch()+3600,'{}')`)
	check(true)
	exec(`UPDATE webhook_subscriptions SET enabled=0`)
	check(false)
}
