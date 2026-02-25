package review

import "database/sql"

func FetchCalibrationData(db *sql.DB, userID string, repo string) (*CalibrationData, error) {
	var totalReviewed, totalApproved, totalRejected int

	err := db.QueryRow(`
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE feedback_type = 'approve'),
			COUNT(*) FILTER (WHERE feedback_type = 'reject')
		FROM review_feedback rf
		JOIN pr_reviews r ON rf.review_id = r.id
		WHERE r.user_id = $1 AND r.repo = $2
	`, userID, repo).Scan(&totalReviewed, &totalApproved, &totalRejected)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	cal := &CalibrationData{
		TotalReviewed:               totalReviewed,
		TotalApproved:               totalApproved,
		TotalRejected:               totalRejected,
		FalsePositiveRateBySeverity: make(map[string]float64),
		FalsePositiveRateByCategory: make(map[string]float64),
	}

	if totalReviewed < 10 {
		return cal, nil
	}

	severityRows, err := db.Query(`
		SELECT rc.severity,
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE rf.feedback_type = 'reject') AS rejected
		FROM review_feedback rf
		JOIN review_comments rc ON rf.comment_id = rc.id
		JOIN pr_reviews r ON rf.review_id = r.id
		WHERE r.user_id = $1 AND r.repo = $2
		GROUP BY rc.severity
	`, userID, repo)
	if err != nil {
		return cal, nil
	}
	defer severityRows.Close()

	for severityRows.Next() {
		var severity string
		var total, rejected int
		if err := severityRows.Scan(&severity, &total, &rejected); err != nil {
			continue
		}
		if total > 0 {
			cal.FalsePositiveRateBySeverity[severity] = float64(rejected) / float64(total)
		}
	}

	categoryRows, err := db.Query(`
		SELECT rc.category,
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE rf.feedback_type = 'reject') AS rejected
		FROM review_feedback rf
		JOIN review_comments rc ON rf.comment_id = rc.id
		JOIN pr_reviews r ON rf.review_id = r.id
		WHERE r.user_id = $1 AND r.repo = $2
		GROUP BY rc.category
	`, userID, repo)
	if err != nil {
		return cal, nil
	}
	defer categoryRows.Close()

	for categoryRows.Next() {
		var category string
		var total, rejected int
		if err := categoryRows.Scan(&category, &total, &rejected); err != nil {
			continue
		}
		if total > 0 {
			cal.FalsePositiveRateByCategory[category] = float64(rejected) / float64(total)
		}
	}

	return cal, nil
}
