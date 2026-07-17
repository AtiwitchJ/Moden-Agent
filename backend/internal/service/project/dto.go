package project

import "github.com/modernagent/modern-agent/backend/internal/domain"

// GetResult is the discriminated result returned by Service.Get.
type GetResult struct {
	Status   string
	Project  *Project
	Degraded *Degraded
}

// AddInput is the body shape for POST /api/v1/projects.
type AddInput struct {
	Path        string                `json:"path"`
	ProjectID   *string               `json:"projectId,omitempty"`
	Name        *string               `json:"name,omitempty"`
	Config      *domain.ProjectConfig `json:"config,omitempty"`
	AsWorkspace bool                  `json:"asWorkspace,omitempty"`
	// AsDocsRepo registers a docs-repo project: git worktree like single_repo, but
	// the deliverable watcher (not PR/CI) drives the session's completed status.
	AsDocsRepo bool `json:"asDocsRepo,omitempty"`
}

// SetConfigInput is the body shape for PUT /api/v1/projects/{id}/config. Config
// replaces the project's stored config wholesale; a zero-value config clears it.
type SetConfigInput struct {
	Config domain.ProjectConfig `json:"config"`
}

// UpdateWorkboardAutonomousInput is a sparse update for a project's
// autonomous workboard settings. Omitted fields retain their stored values.
type UpdateWorkboardAutonomousInput struct {
	Enabled             *bool
	Mode                *string
	ShortTimeoutMinutes *int
	Sticky              *bool
}

func (in UpdateWorkboardAutonomousInput) toPatch() domain.WorkboardAutonomousPatch {
	return domain.WorkboardAutonomousPatch{
		Enabled: in.Enabled, Mode: in.Mode, ShortTimeoutMinutes: in.ShortTimeoutMinutes, Sticky: in.Sticky,
	}
}

// RemoveResult reports what DELETE /api/v1/projects/{id} actually did.
type RemoveResult struct {
	ProjectID         domain.ProjectID `json:"projectId"`
	RemovedStorageDir bool             `json:"removedStorageDir"`
}
