package goshared

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/adhocore/gronx"
)

const (
	MaxNextRuns            = 100
	MinimumIntervalMinutes = 15
)

var (
	validatedTimezones sync.Map
	daysOfWeek         = []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
	simpleIntegerRegex = regexp.MustCompile(`^\d+$`)
)

func ValidateTimezone(timezone string) bool {
	if _, ok := validatedTimezones.Load(timezone); ok {
		return true
	}

	_, err := time.LoadLocation(timezone)
	if err != nil {
		return false
	}

	validatedTimezones.Store(timezone, true)
	return true
}

func ValidateCronExpression(cronExpression string) (bool, string) {
	gron := gronx.New()
	if !gron.IsValid(cronExpression) {
		return false, "Invalid cron expression"
	}
	return true, ""
}

func ValidateCronFrequency(cronExpression string) (bool, string) {
	gron := gronx.New()
	if !gron.IsValid(cronExpression) {
		return false, "Invalid cron expression"
	}

	now := time.Now()
	first, err := gronx.NextTickAfter(cronExpression, now, false)
	if err != nil {
		return false, "Cannot compute next occurrence"
	}

	second, err := gronx.NextTickAfter(cronExpression, first, false)
	if err != nil {
		return false, "Cannot compute second occurrence"
	}

	intervalMinutes := second.Sub(first).Minutes()
	if intervalMinutes < MinimumIntervalMinutes {
		return false, "Schedule must run at most every 15 minutes"
	}

	return true, ""
}

func ComputeNextRunAt(cronExpression string, timezone string, after *time.Time) (time.Time, error) {
	if !ValidateTimezone(timezone) {
		return time.Time{}, fmt.Errorf("invalid timezone: %s", timezone)
	}

	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to load timezone: %w", err)
	}

	gron := gronx.New()
	if !gron.IsValid(cronExpression) {
		return time.Time{}, fmt.Errorf("invalid cron expression")
	}

	var refTime time.Time
	if after != nil {
		refTime = after.In(loc)
	} else {
		refTime = time.Now().In(loc)
	}

	nextTick, err := gronx.NextTickAfter(cronExpression, refTime, false)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to compute next run: %w", err)
	}

	return nextTick.In(loc), nil
}

func GetNextRuns(cronExpression string, timezone string, count int) ([]time.Time, error) {
	if !ValidateTimezone(timezone) {
		return nil, fmt.Errorf("invalid timezone: %s", timezone)
	}

	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("failed to load timezone: %w", err)
	}

	gron := gronx.New()
	if !gron.IsValid(cronExpression) {
		return nil, fmt.Errorf("invalid cron expression")
	}

	boundedCount := count
	if boundedCount > MaxNextRuns {
		boundedCount = MaxNextRuns
	}
	if boundedCount < 0 {
		boundedCount = 0
	}

	runs := make([]time.Time, 0, boundedCount)
	current := time.Now().In(loc)

	for i := 0; i < boundedCount; i++ {
		nextTick, err := gronx.NextTickAfter(cronExpression, current, false)
		if err != nil {
			return nil, fmt.Errorf("failed to compute next run: %w", err)
		}
		runs = append(runs, nextTick.In(loc))
		current = nextTick
	}

	return runs, nil
}

func isSimpleInteger(value string) bool {
	return simpleIntegerRegex.MatchString(value)
}

func formatTime(hour, minute int) string {
	period := "AM"
	displayHour := hour
	if hour >= 12 {
		period = "PM"
	}
	if hour == 0 {
		displayHour = 12
	} else if hour > 12 {
		displayHour = hour - 12
	}
	return fmt.Sprintf("%d:%02d %s", displayHour, minute, period)
}

func DescribeCronExpression(cronExpression string) string {
	parts := strings.Fields(strings.TrimSpace(cronExpression))
	if len(parts) != 5 {
		return cronExpression
	}

	minute := parts[0]
	hour := parts[1]
	dayOfMonth := parts[2]
	month := parts[3]
	dayOfWeek := parts[4]

	if isSimpleInteger(minute) && isSimpleInteger(hour) && dayOfMonth == "*" && month == "*" {
		hourNum, err1 := strconv.Atoi(hour)
		minuteNum, err2 := strconv.Atoi(minute)
		if err1 != nil || err2 != nil {
			return cronExpression
		}
		timeStr := formatTime(hourNum, minuteNum)

		if dayOfWeek == "*" {
			return fmt.Sprintf("Every day at %s", timeStr)
		}

		if dayOfWeek == "1-5" {
			return fmt.Sprintf("Every weekday at %s", timeStr)
		}

		if dayOfWeek == "0,6" {
			return fmt.Sprintf("Every weekend at %s", timeStr)
		}

		if isSimpleInteger(dayOfWeek) {
			dayNum, err := strconv.Atoi(dayOfWeek)
			if err == nil && dayNum >= 0 && dayNum <= 6 {
				return fmt.Sprintf("Every %s at %s", daysOfWeek[dayNum], timeStr)
			}
		}
	}

	if isSimpleInteger(minute) && hour == "*" && dayOfMonth == "*" && month == "*" && dayOfWeek == "*" {
		return fmt.Sprintf("Every hour at minute %s", minute)
	}

	if minute == "*" && hour == "*" && dayOfMonth == "*" && month == "*" && dayOfWeek == "*" {
		return "Every minute"
	}

	if strings.Contains(hour, "/") {
		parts := strings.Split(hour, "/")
		if len(parts) == 2 && isSimpleInteger(parts[1]) {
			return fmt.Sprintf("Every %s hours", parts[1])
		}
	}

	if strings.Contains(minute, "/") {
		parts := strings.Split(minute, "/")
		if len(parts) == 2 && isSimpleInteger(parts[1]) {
			return fmt.Sprintf("Every %s minutes", parts[1])
		}
	}

	return cronExpression
}
