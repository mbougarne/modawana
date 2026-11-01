package learning

import (
	"context"
	"time"
)

type Progress struct {
	Key        string `json:"key"`
	Collection string `json:"collection"`
	Epoch      int64  `json:"epoch"`
	Method     string `json:"method"`
}

func (s *IdentityService) Progress(ctx context.Context, id string) ([]Progress, error) {
	out := []Progress{}
	rows, e := s.DB.QueryContext(ctx, `SELECT i.key,coalesce(c.key,''),p.epoch,p.method FROM learning_completion p JOIN content_items i ON i.id=p.item_id LEFT JOIN collections c ON c.id=p.collection_id LEFT JOIN collection_plan_state st ON st.collection_id=c.id WHERE p.identity_id=? AND (p.collection_id IS NULL OR p.epoch=st.epoch) ORDER BY c.key,i.key LIMIT 10000`, id)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var p Progress
		if e = rows.Scan(&p.Key, &p.Collection, &p.Epoch, &p.Method); e != nil {
			return out, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Additional per-IP ceiling, hashed and bounded in memory. Identity limits are
// enforced transactionally in SQLite; restarting cannot reset those limits.
func (s *IdentityService) AllowAttempt(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	for key, b := range s.buckets {
		if now.Sub(b.start) >= time.Minute {
			delete(s.buckets, key)
		}
	}
	key := digest([]byte("attempt:" + ip))
	b := s.buckets[key]
	if b.count >= 100 || b.count == 0 && len(s.buckets) >= 4096 {
		return false
	}
	if b.count == 0 {
		b.start = now
	}
	b.count++
	s.buckets[key] = b
	return true
}
