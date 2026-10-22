package identity

import "time"

// RecentlyAuthenticated accepts sessions created within the last five minutes.
func RecentlyAuthenticated(session Session, now time.Time) bool {
	return session.ID != "" && !session.CreatedAt.Before(now.Add(-5*time.Minute)) && !session.CreatedAt.After(now.Add(time.Minute))
}
