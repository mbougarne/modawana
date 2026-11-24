package analytics

import (
	"context"
	"database/sql"
	"time"
)

type Totals struct {
	PageViews int64 `json:"page_views"`
	Leads     int64 `json:"leads"`
}

type Day struct {
	Day string `json:"day"`
	Totals
}

type Page struct {
	Path string `json:"path"`
	Totals
}

type Report struct {
	Settings Settings `json:"settings"`
	Totals   Totals   `json:"totals"`
	Days     []Day    `json:"days"`
	Pages    []Page   `json:"pages"`
}

const sumCounts = `coalesce(sum(CASE event WHEN 'page_view' THEN count ELSE 0 END),0),
	coalesce(sum(CASE event WHEN 'lead_submitted' THEN count ELSE 0 END),0)`

func (s *Service) Report(ctx context.Context) (Report, error) {
	s.write.Lock()
	defer s.write.Unlock()
	settings := s.Settings()
	out := Report{Settings: settings, Days: []Day{}, Pages: []Page{}}
	now := s.now()
	before, today := cutoff(now, settings.RetentionDays), day(now)
	// All report totals use the same committed snapshot.
	tx, err := s.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT day,`+sumCounts+` FROM analytics_daily WHERE day BETWEEN ? AND ? GROUP BY day ORDER BY day DESC`, before, today)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var entry Day
		if err = rows.Scan(&entry.Day, &entry.PageViews, &entry.Leads); err != nil {
			rows.Close()
			return out, err
		}
		if _, err = time.Parse(time.DateOnly, entry.Day); err != nil {
			rows.Close()
			return out, ErrInvalid
		}
		out.Totals.PageViews += entry.PageViews
		out.Totals.Leads += entry.Leads
		out.Days = append(out.Days, entry)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT path,`+sumCounts+` FROM analytics_daily WHERE day BETWEEN ? AND ? GROUP BY path ORDER BY 2 DESC,3 DESC,path LIMIT 30`, before, today)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var entry Page
		if err = rows.Scan(&entry.Path, &entry.PageViews, &entry.Leads); err != nil {
			return out, err
		}
		out.Pages = append(out.Pages, entry)
	}
	return out, rows.Err()
}
