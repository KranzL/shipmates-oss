package delegation

import (
	"context"
	"fmt"
	"strings"
	"time"

	goshared "github.com/KranzL/shipmates-oss/go-shared"
)

const (
	lockExpiryDuration = 30 * time.Minute
	maxFilesPerLock    = 100
)

type LockResult struct {
	Acquired    bool
	Conflicting []string
}

func hasPathTraversal(path string) bool {
	normalized := strings.ReplaceAll(path, "\\", "/")
	if strings.Contains(normalized, "\x00") {
		return true
	}
	if strings.HasPrefix(normalized, "/") {
		return true
	}
	if strings.HasPrefix(normalized, "../") || strings.Contains(normalized, "/../") {
		return true
	}
	segments := strings.Split(normalized, "/")
	for _, s := range segments {
		if s == ".." {
			return true
		}
	}
	return false
}

func ClaimLocks(ctx context.Context, db *goshared.DB, userID, sessionID, agentID string, filePaths []string) (*LockResult, error) {
	if len(filePaths) == 0 {
		return &LockResult{Acquired: true}, nil
	}

	if len(filePaths) > maxFilesPerLock {
		return &LockResult{
			Acquired:    false,
			Conflicting: []string{fmt.Sprintf("exceeds max lockable files (%d)", maxFilesPerLock)},
		}, nil
	}

	var sanitized []string
	for _, path := range filePaths {
		if !hasPathTraversal(path) {
			sanitized = append(sanitized, path)
		}
	}

	if len(sanitized) != len(filePaths) {
		return &LockResult{
			Acquired:    false,
			Conflicting: []string{"invalid file paths detected"},
		}, nil
	}

	if _, err := db.CleanExpiredLocks(ctx); err != nil {
		fmt.Printf("[delegation-locks] failed to clean expired locks: %v\n", err)
	}

	existing, err := db.GetActiveLocks(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get active locks: %w", err)
	}

	lockMap := make(map[string]bool)
	for _, lock := range existing {
		if lock.SessionID != sessionID {
			lockMap[lock.FilePath] = true
		}
	}

	var conflicting []string
	for _, path := range sanitized {
		if lockMap[path] {
			conflicting = append(conflicting, path)
		}
	}

	if len(conflicting) > 0 {
		return &LockResult{
			Acquired:    false,
			Conflicting: conflicting,
		}, nil
	}

	for _, path := range sanitized {
		acquired, err := db.AcquireTaskLock(ctx, userID, sessionID, agentID, path, lockExpiryDuration)
		if err != nil {
			fmt.Printf("[delegation-locks] failed to acquire lock for %s: %v\n", path, err)
			continue
		}
		if !acquired {
			existing, recheckErr := db.GetActiveLocks(ctx, userID)
			if recheckErr != nil {
				return &LockResult{
					Acquired:    false,
					Conflicting: []string{path},
				}, nil
			}

			recheckMap := make(map[string]bool)
			for _, lock := range existing {
				if lock.SessionID != sessionID {
					recheckMap[lock.FilePath] = true
				}
			}

			var recheckConflicting []string
			for _, p := range sanitized {
				if recheckMap[p] {
					recheckConflicting = append(recheckConflicting, p)
				}
			}

			if len(recheckConflicting) > 0 {
				query := `DELETE FROM task_locks WHERE session_id = $1`
				db.ExecContext(ctx, query, sessionID)

				return &LockResult{
					Acquired:    false,
					Conflicting: recheckConflicting,
				}, nil
			}
		}
	}

	return &LockResult{Acquired: true}, nil
}

func ReleaseLocks(ctx context.Context, db *goshared.DB, sessionID, userID string) error {
	return db.ReleaseSessionLocks(ctx, sessionID, userID)
}

func CheckLocks(ctx context.Context, db *goshared.DB, userID string, filePaths []string) (bool, []string, error) {
	if len(filePaths) == 0 {
		return false, []string{}, nil
	}

	var sanitized []string
	for _, path := range filePaths {
		if !hasPathTraversal(path) {
			sanitized = append(sanitized, path)
		}
	}

	if len(sanitized) == 0 {
		return false, []string{}, nil
	}

	if _, err := db.CleanExpiredLocks(ctx); err != nil {
		fmt.Printf("[delegation-locks] failed to clean expired locks: %v\n", err)
	}

	existing, err := db.GetActiveLocks(ctx, userID)
	if err != nil {
		return false, []string{}, fmt.Errorf("get active locks: %w", err)
	}

	lockMap := make(map[string]bool)
	for _, lock := range existing {
		lockMap[lock.FilePath] = true
	}

	var lockedFiles []string
	for _, path := range sanitized {
		if lockMap[path] {
			lockedFiles = append(lockedFiles, path)
		}
	}

	return len(lockedFiles) > 0, lockedFiles, nil
}
