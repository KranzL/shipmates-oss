package delegation

import (
	"context"
	"fmt"
	"regexp"

	goshared "github.com/KranzL/shipmates-oss/go-shared"
)

const (
	descriptionMinLength = 1
	descriptionMaxLength = 2000
	maxDepthQueries      = 20
)

var (
	roleHierarchy = map[string]int{
		"lead":   2,
		"senior": 1,
		"junior": 0,
	}

	roleCanDelegateTo = map[string]map[string]bool{
		"lead":   {"senior": true, "junior": true},
		"senior": {"junior": true},
		"junior": {},
	}
)

type AgentForValidation struct {
	ID                 string
	Role               string
	MaxCostPerTask     float64
	MaxDelegationDepth int
	CanDelegateTo      []string
}

type ValidationResult struct {
	Allowed bool
	Reason  string
}

func ValidateDelegation(fromAgent, toAgent AgentForValidation, description string, maxCostUSD *float64) *ValidationResult {
	if fromAgent.ID == toAgent.ID {
		return &ValidationResult{
			Allowed: false,
			Reason:  "An agent cannot delegate work to itself. Choose a different target agent.",
		}
	}

	fromLevel, fromLevelExists := roleHierarchy[fromAgent.Role]
	if !fromLevelExists || fromLevel == 0 {
		return &ValidationResult{
			Allowed: false,
			Reason:  "Junior agents cannot delegate work. Only senior and lead agents can delegate. Change the agent's role to senior or lead in the dashboard.",
		}
	}

	allowedTargets, allowedExists := roleCanDelegateTo[fromAgent.Role]
	if !allowedExists || !allowedTargets[toAgent.Role] {
		var reason string
		if fromAgent.Role == "senior" {
			reason = "Senior agents can only delegate to junior agents. Choose a junior agent."
		} else {
			reason = "Lead agents can delegate to senior or junior agents. Choose an appropriate target agent."
		}
		return &ValidationResult{
			Allowed: false,
			Reason:  fmt.Sprintf("A %s agent cannot delegate to a %s agent. %s", fromAgent.Role, toAgent.Role, reason),
		}
	}

	if len(fromAgent.CanDelegateTo) > 0 {
		allowed := false
		for _, id := range fromAgent.CanDelegateTo {
			if id == toAgent.ID {
				allowed = true
				break
			}
		}
		if !allowed {
			return &ValidationResult{
				Allowed: false,
				Reason:  "This agent is not authorized to delegate to the selected target agent. Add the target agent to the delegation allowlist in the dashboard under agent settings.",
			}
		}
	}

	if maxCostUSD != nil && *maxCostUSD > fromAgent.MaxCostPerTask {
		return &ValidationResult{
			Allowed: false,
			Reason:  fmt.Sprintf("Task cost limit ($%.2f) exceeds this agent's maximum allowed cost ($%.2f). Either lower the cost limit for this task or increase the agent's maximum cost in the dashboard.", *maxCostUSD, fromAgent.MaxCostPerTask),
		}
	}

	trimmedDescription := ""
	if description != "" {
		trimmedDescription = description
	}
	if len(trimmedDescription) < descriptionMinLength {
		return &ValidationResult{
			Allowed: false,
			Reason:  "Task description is required and cannot be empty. Provide a clear description of what the agent should do.",
		}
	}

	if len(trimmedDescription) > descriptionMaxLength {
		return &ValidationResult{
			Allowed: false,
			Reason:  fmt.Sprintf("Task description is too long (%d characters). Maximum is %d characters. Try breaking this into multiple smaller tasks.", len(trimmedDescription), descriptionMaxLength),
		}
	}

	return &ValidationResult{Allowed: true}
}

type DelegationChainRow struct {
	ParentSessionID string
	ChildSessionID  string
}

func ValidateDelegationDepth(ctx context.Context, db *goshared.DB, parentSessionID, userID string, maxDepth int) (*ValidationResult, error) {
	if parentSessionID == "" {
		return &ValidationResult{Allowed: true}, nil
	}

	if maxDepth == 0 {
		return &ValidationResult{
			Allowed: false,
			Reason:  "This agent cannot delegate. Change the agent's role to allow delegation.",
		}, nil
	}

	sessionIDRegex := regexp.MustCompile(`^[a-z0-9]{20,36}$`)
	if !sessionIDRegex.MatchString(parentSessionID) {
		return &ValidationResult{Allowed: true}, nil
	}

	parentExists, err := db.GetSession(ctx, parentSessionID, userID)
	if err != nil {
		return nil, fmt.Errorf("check parent session: %w", err)
	}
	if parentExists == nil {
		return &ValidationResult{
			Allowed: false,
			Reason:  "Parent session not found. The delegating session may have been deleted.",
		}, nil
	}

	query := `
		WITH RECURSIVE chain AS (
			SELECT parent_session_id, child_session_id, 1 as depth
			FROM delegated_tasks
			WHERE child_session_id = $1 AND user_id = $2

			UNION ALL

			SELECT dt.parent_session_id, dt.child_session_id, c.depth + 1
			FROM delegated_tasks dt
			INNER JOIN chain c ON dt.child_session_id = c.parent_session_id
			WHERE c.depth < $3 AND dt.user_id = $2
		)
		SELECT parent_session_id, child_session_id FROM chain
	`

	rows, err := db.QueryContext(ctx, query, parentSessionID, userID, maxDepthQueries)
	if err != nil {
		return nil, fmt.Errorf("query delegation chain: %w", err)
	}
	defer rows.Close()

	var allAncestors []DelegationChainRow
	for rows.Next() {
		var row DelegationChainRow
		var parentSessionIDNull, childSessionIDNull *string

		if err := rows.Scan(&parentSessionIDNull, &childSessionIDNull); err != nil {
			return nil, fmt.Errorf("scan chain row: %w", err)
		}

		if parentSessionIDNull != nil {
			row.ParentSessionID = *parentSessionIDNull
		}
		if childSessionIDNull != nil {
			row.ChildSessionID = *childSessionIDNull
		}

		allAncestors = append(allAncestors, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate chain: %w", err)
	}

	if len(allAncestors) == 0 {
		return &ValidationResult{Allowed: true}, nil
	}

	visited := make(map[string]bool)
	visited[parentSessionID] = true

	for _, ancestor := range allAncestors {
		if ancestor.ChildSessionID != "" && visited[ancestor.ChildSessionID] {
			return &ValidationResult{
				Allowed: false,
				Reason:  "Circular delegation detected. This would create an infinite loop.",
			}, nil
		}
		if ancestor.ChildSessionID != "" {
			visited[ancestor.ChildSessionID] = true
		}
	}

	currentDepth := len(allAncestors)
	if currentDepth >= maxDepth {
		return &ValidationResult{
			Allowed: false,
			Reason:  fmt.Sprintf("Maximum delegation depth (%d) exceeded. This prevents chains from becoming too deep.", maxDepth),
		}, nil
	}

	return &ValidationResult{Allowed: true}, nil
}

type TaskForCycle struct {
	ID        string
	BlockedBy []string
}

func DetectCyclesInBlockedBy(taskID string, blockedBy []string, existingTasks []TaskForCycle) bool {
	adjacency := make(map[string][]string)

	for _, task := range existingTasks {
		adjacency[task.ID] = append([]string{}, task.BlockedBy...)
	}
	adjacency[taskID] = append([]string{}, blockedBy...)

	visited := make(map[string]bool)
	inStack := make(map[string]bool)

	var hasCycle func(nodeID string) bool
	hasCycle = func(nodeID string) bool {
		if inStack[nodeID] {
			return true
		}
		if visited[nodeID] {
			return false
		}

		visited[nodeID] = true
		inStack[nodeID] = true

		dependencies := adjacency[nodeID]
		for _, depID := range dependencies {
			if hasCycle(depID) {
				return true
			}
		}

		delete(inStack, nodeID)
		return false
	}

	return hasCycle(taskID)
}
