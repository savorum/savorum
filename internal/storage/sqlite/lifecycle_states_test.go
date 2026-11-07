package sqlite

import (
	"strings"
	"testing"
)

func TestLifecycleColumnsRefuseUndefinedStates(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(ctx, t)

	for _, write := range []string{
		`INSERT INTO assistant_requests(id,user_code,request,expires_at,status) VALUES('r','CODE','{}',0,'expired')`,
		`INSERT INTO automation_jobs(id,library_id,actor_id,kind,state,revision,created_at,expires_at,record_json) VALUES('j','l','u','import','complete',1,0,0,'{}')`,
		`INSERT INTO webhook_deliveries(id,subscription_id,event_id,event_type,state,created_at,cycle_at,body) VALUES('d','s','e','recipe.created','sent',0,0,'{}')`,
	} {
		_, err := store.db.ExecContext(ctx, write)
		if err == nil || !strings.Contains(err.Error(), "is not a lifecycle state") {
			t.Fatalf("%s: error = %v", write, err)
		}
	}

	if _, err := store.db.ExecContext(ctx, `INSERT INTO assistant_requests(id,user_code,request,expires_at) VALUES('r','CODE','{}',0)`); err != nil {
		t.Fatal(err)
	}

	if _, err := store.db.ExecContext(ctx, `UPDATE assistant_requests SET status='expired' WHERE id='r'`); err == nil || !strings.Contains(err.Error(), "is not a lifecycle state") {
		t.Fatalf("update error = %v", err)
	}
}
