package store

import (
	"context"
	"time"

	"exta/internal/domain"
)

// CallTrends counts retained logical calls once, using their current execution outcome.
// A single SQL statement observes requests and research jobs in the same snapshot.
func (state *Store) CallTrends(ctx context.Context, hours int) (domain.CallTrends, error) {
	result := domain.CallTrends{Coverage: "retained_accepted_calls", Buckets: []domain.CallTrendBucket{}}
	if hours != 24 && hours != 168 && hours != 720 {
		return result, Reject("invalid_request", 400, "Choose a 24, 168 or 720 hour trend range")
	}
	width := time.Hour
	if hours == 720 {
		width = 24 * time.Hour
	}
	now := state.Now().UTC()
	count := time.Duration(hours) * time.Hour / width
	start := now.Truncate(width).Add(-(count - 1) * width)
	result.ObservedAt, result.From, result.To = Stamp(now), Stamp(start), Stamp(now)
	result.BucketSeconds = domain.Integer(width / time.Second)
	for index := range int(count) {
		result.Buckets = append(result.Buckets, domain.CallTrendBucket{Start: Stamp(start.Add(time.Duration(index) * width))})
	}
	rows, err := state.DB.QueryContext(ctx, `WITH calls AS (
		SELECT created_at,json_extract(data,'$.status') AS status FROM requests WHERE created_at>=? AND created_at<?
		UNION ALL
		SELECT created_at,state AS status FROM research_jobs WHERE created_at>=? AND created_at<?
	) SELECT CAST(strftime('%s',created_at) AS INTEGER)/?,status,count(*) FROM calls GROUP BY 1,2`, result.From, result.To, result.From, result.To, int64(width/time.Second))
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var epoch, amount int64
		var status string
		if err = rows.Scan(&epoch, &status, &amount); err != nil {
			return result, err
		}
		index := epoch - start.Unix()/int64(width/time.Second)
		if index < 0 || index >= int64(len(result.Buckets)) {
			continue
		}
		bucket := &result.Buckets[index]
		bucket.Total += domain.Integer(amount)
		switch status {
		case "succeeded":
			bucket.Succeeded += domain.Integer(amount)
		case "failed":
			bucket.Failed += domain.Integer(amount)
		case "uncertain", "submission_unknown", "observation_expired":
			bucket.Uncertain += domain.Integer(amount)
		case "cancelled":
			bucket.Cancelled += domain.Integer(amount)
		default:
			bucket.Pending += domain.Integer(amount)
		}
	}
	return result, rows.Err()
}
