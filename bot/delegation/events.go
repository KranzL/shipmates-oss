package delegation

import (
	"context"
	"fmt"
	"regexp"

	goshared "github.com/KranzL/shipmates-oss/go-shared"
)

var controlCharRegex = regexp.MustCompile(`[\x00-\x1F\x7F]`)
var nonPrintableRegex = regexp.MustCompile(`[^\x20-\x7E\n]`)

type LogDelegationOptions struct {
	UserID        string
	FromSessionID string
	ToSessionID   *string
	FromAgentID   string
	ToAgentID     string
	Action        string
	Reason        string
	CostUSD       *float64
}

func LogDelegation(ctx context.Context, db *goshared.DB, opts LogDelegationOptions) error {
	sanitizedReason := opts.Reason
	sanitizedReason = controlCharRegex.ReplaceAllString(sanitizedReason, "")
	sanitizedReason = nonPrintableRegex.ReplaceAllString(sanitizedReason, "")

	if len(sanitizedReason) > 4000 {
		sanitizedReason = sanitizedReason[:4000]
	}

	var reasonPtr *string
	if sanitizedReason != "" {
		reasonPtr = &sanitizedReason
	}

	if opts.CostUSD != nil && (*opts.CostUSD < 0 || *opts.CostUSD > 10000) {
		fmt.Printf("[delegation-events] Invalid costUsd: %.2f for userId %s\n", *opts.CostUSD, opts.UserID)
		return nil
	}

	event := &goshared.DelegationEvent{
		UserID:        opts.UserID,
		FromSessionID: opts.FromSessionID,
		ToSessionID:   opts.ToSessionID,
		FromAgentID:   opts.FromAgentID,
		ToAgentID:     opts.ToAgentID,
		Action:        opts.Action,
		Reason:        reasonPtr,
		CostUSD:       opts.CostUSD,
	}

	if err := db.CreateDelegationEvent(ctx, event); err != nil {
		return fmt.Errorf("create delegation event: %w", err)
	}

	return nil
}
