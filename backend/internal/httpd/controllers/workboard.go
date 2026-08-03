package controllers

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/httpd/apierr"
	"github.com/modernagent/modern-agent/backend/internal/httpd/apispec"
	"github.com/modernagent/modern-agent/backend/internal/httpd/envelope"
	projectsvc "github.com/modernagent/modern-agent/backend/internal/service/project"
	workboardsvc "github.com/modernagent/modern-agent/backend/internal/service/workboard"
)

// WorkboardService is the controller-facing work-card CRUD boundary.
type WorkboardService interface {
	Create(ctx context.Context, in workboardsvc.CreateInput) (domain.WorkCard, error)
	List(ctx context.Context, projectID, boardID string) ([]domain.WorkCard, error)
	ListAll(ctx context.Context) ([]domain.WorkCard, error)
	Get(ctx context.Context, id string) (domain.WorkCard, error)
	Delete(ctx context.Context, id string) error
	Update(ctx context.Context, id string, in workboardsvc.UpdateInput) (domain.WorkCard, error)
	Move(ctx context.Context, id string, status domain.CardStatus, position int64) (domain.WorkCard, error)
	Nudge(ctx context.Context, id string, in workboardsvc.NudgeInput) (domain.WorkCard, error)
	Retarget(ctx context.Context, id string, in workboardsvc.RetargetInput) (domain.WorkCard, error)
	Split(ctx context.Context, id string, in workboardsvc.SplitInput) (workboardsvc.SplitResult, error)
	RecordAgentEvent(ctx context.Context, cardID string, in workboardsvc.AgentEventInput) (domain.WorkCard, error)
	Handoffs(ctx context.Context, cardID string) ([]workboardsvc.Handoff, error)
	ListRedo(ctx context.Context, cardID string) ([]domain.RedoCycle, error)
	LatestDispatchFailure(ctx context.Context, cardID string) (workboardsvc.DispatchFailure, error)
	DirectorStatus(ctx context.Context, projectID string) (workboardsvc.DirectorStatus, error)
}

// WorkboardController owns the project-scoped work-card routes.
type WorkboardController struct {
	Svc            WorkboardService
	Projects       projectsvc.Manager
	DispatchKicker workboardsvc.DispatchKicker
	StatusProvider workboardsvc.DirectorStatusProvider
}

// Register mounts the workboard routes on the supplied router.
func (c *WorkboardController) Register(r chi.Router) {
	r.Get("/workboard/cards", c.listGlobal)
	r.Post("/workboard/cards", c.createGlobal)
	r.Get("/workboard/cards/{cardId}/redo", c.listRedo)
	r.Get("/workboard/cards/{cardId}/dispatch-failure", c.dispatchFailure)
	r.Get("/projects/{projectId}/workboard/cards", c.list)
	r.Post("/projects/{projectId}/workboard/cards", c.create)
	r.Post("/projects/{projectId}/workboard/dispatch", c.dispatch)
	r.Get("/projects/{projectId}/workboard/director-status", c.directorStatus)
	r.Patch("/projects/{id}/workboard/autonomous", c.updateAutonomous)
	r.Get("/workboard/cards/{cardId}", c.get)
	r.Delete("/workboard/cards/{cardId}", c.delete)
	r.Patch("/workboard/cards/{cardId}", c.update)
	r.Post("/workboard/cards/{cardId}/move", c.move)
	r.Post("/workboard/cards/{cardId}/nudge", c.nudge)
	r.Post("/workboard/cards/{cardId}/retarget", c.retarget)
	r.Post("/workboard/cards/{cardId}/split", c.split)
	r.Post("/workboard/cards/{cardId}/events", c.recordEvent)
}

func (c *WorkboardController) dispatchFailure(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, http.MethodGet, "/api/v1/workboard/cards/{cardId}/dispatch-failure")
		return
	}
	failure, err := c.Svc.LatestDispatchFailure(r.Context(), chi.URLParam(r, "cardId"))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, DispatchFailureResponse{
		CardID: failure.CardID, Reason: failure.Reason, AttemptedAt: failure.AttemptedAt,
	})
}

func (c *WorkboardController) updateAutonomous(w http.ResponseWriter, r *http.Request) {
	if c.Projects == nil {
		apispec.NotImplemented(w, r, http.MethodPatch, "/api/v1/projects/{id}/workboard/autonomous")
		return
	}
	var req UpdateWorkboardAutonomousRequest
	if err := decodeJSONStrict(r, &req); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Invalid JSON body", nil)
		return
	}
	id := projectID(r)
	updated, err := c.Projects.UpdateWorkboardAutonomous(r.Context(), id, req.toInput())
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	if updated.Config == nil {
		envelope.WriteAPIError(w, r, http.StatusInternalServerError, "internal", "WORKBOARD_AUTONOMOUS_UPDATE_FAILED", "Autonomous config update did not persist", nil)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, WorkboardAutonomousResponse{
		ProjectID:  string(id),
		Autonomous: updated.Config.Workboard.Autonomous,
	})
}

func (c *WorkboardController) list(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, http.MethodGet, "/api/v1/projects/{projectId}/workboard/cards")
		return
	}
	cards, err := c.Svc.List(r.Context(), chi.URLParam(r, "projectId"), "")
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, ListWorkCardsResponse{Cards: workCardResponses(cards)})
}

func (c *WorkboardController) create(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, http.MethodPost, "/api/v1/projects/{projectId}/workboard/cards")
		return
	}
	var req CreateWorkCardRequest
	if err := decodeJSONStrict(r, &req); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Invalid JSON body", nil)
		return
	}
	card, err := c.Svc.Create(r.Context(), req.toInput(chi.URLParam(r, "projectId")))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusCreated, newWorkCardResponse(card))
}

func (c *WorkboardController) dispatch(w http.ResponseWriter, r *http.Request) {
	if c.DispatchKicker == nil {
		apispec.NotImplemented(w, r, http.MethodPost, "/api/v1/projects/{projectId}/workboard/dispatch")
		return
	}
	projectID := chi.URLParam(r, "projectId")
	if projectID == "" {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "PROJECT_ID_REQUIRED", "Project ID is required", nil)
		return
	}
	c.DispatchKicker.Kick(projectID)
	envelope.WriteJSON(w, http.StatusAccepted, DispatchWorkboardResponse{ProjectID: projectID, Dispatched: true})
}

func (c *WorkboardController) directorStatus(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, http.MethodGet, "/api/v1/projects/{projectId}/workboard/director-status")
		return
	}
	status, err := c.Svc.DirectorStatus(r.Context(), chi.URLParam(r, "projectId"))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	resp := DirectorStatusResponse{
		ProjectID:    status.ProjectID,
		DaemonReady:  status.DaemonReady,
		RunningCount: status.RunningCount,
		WIPLimit:     status.WIPLimit,
		TodoCount:    status.TodoCount,
	}
	if !status.LastDispatchAttempt.AttemptedAt.IsZero() {
		resp.LastDispatchAttempt = DispatchAttemptResponse{
			AttemptedAt: status.LastDispatchAttempt.AttemptedAt,
			Result:      status.LastDispatchAttempt.Result,
			Error:       status.LastDispatchAttempt.Error,
		}
	}
	envelope.WriteJSON(w, http.StatusOK, resp)
}

func (c *WorkboardController) get(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, http.MethodGet, "/api/v1/workboard/cards/{cardId}")
		return
	}
	card, err := c.Svc.Get(r.Context(), chi.URLParam(r, "cardId"))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	handoffs, err := c.Svc.Handoffs(r.Context(), card.ID)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	response := newWorkCardResponse(card)
	response.Handoffs = workCardHandoffResponses(handoffs)
	envelope.WriteJSON(w, http.StatusOK, response)
}

func (c *WorkboardController) delete(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, http.MethodDelete, "/api/v1/workboard/cards/{cardId}")
		return
	}
	cardID := chi.URLParam(r, "cardId")
	if err := c.Svc.Delete(r.Context(), cardID); err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, DeleteWorkCardResponse{OK: true, CardID: cardID})
}

func (c *WorkboardController) update(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, http.MethodPatch, "/api/v1/workboard/cards/{cardId}")
		return
	}
	var req UpdateWorkCardRequest
	if err := decodeJSONStrict(r, &req); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Invalid JSON body", nil)
		return
	}
	card, err := c.Svc.Update(r.Context(), chi.URLParam(r, "cardId"), req.toInput())
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, newWorkCardResponse(card))
}

func (c *WorkboardController) move(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, http.MethodPost, "/api/v1/workboard/cards/{cardId}/move")
		return
	}
	var req MoveWorkCardRequest
	if err := decodeJSONStrict(r, &req); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Invalid JSON body", nil)
		return
	}
	if !req.positionSet {
		envelope.WriteError(w, r, apierr.Invalid("WORK_CARD_POSITION_REQUIRED", "Position is required", nil))
		return
	}
	card, err := c.Svc.Move(r.Context(), chi.URLParam(r, "cardId"), domain.CardStatus(req.Status), req.Position)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, newWorkCardResponse(card))
}

func (c *WorkboardController) nudge(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, http.MethodPost, "/api/v1/workboard/cards/{cardId}/nudge")
		return
	}
	var req NudgeWorkCardRequest
	if err := decodeJSONStrict(r, &req); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Invalid JSON body", nil)
		return
	}
	card, err := c.Svc.Nudge(r.Context(), chi.URLParam(r, "cardId"), req.toInput())
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, newWorkCardResponse(card))
}

func (c *WorkboardController) retarget(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, http.MethodPost, "/api/v1/workboard/cards/{cardId}/retarget")
		return
	}
	var req RetargetWorkCardRequest
	if err := decodeJSONStrict(r, &req); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Invalid JSON body", nil)
		return
	}
	card, err := c.Svc.Retarget(r.Context(), chi.URLParam(r, "cardId"), req.toInput())
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, newWorkCardResponse(card))
}

func (c *WorkboardController) split(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, http.MethodPost, "/api/v1/workboard/cards/{cardId}/split")
		return
	}
	var req SplitWorkCardRequest
	if err := decodeJSONStrict(r, &req); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Invalid JSON body", nil)
		return
	}
	result, err := c.Svc.Split(r.Context(), chi.URLParam(r, "cardId"), req.toInput())
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, SplitWorkCardResponse{
		OldCard: newWorkCardResponse(result.OldCard),
		NewCard: newWorkCardResponse(result.NewCard),
	})
}

func (c *WorkboardController) recordEvent(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, http.MethodPost, "/api/v1/workboard/cards/{cardId}/events")
		return
	}
	var req RecordCardEventRequest
	if err := decodeJSONStrict(r, &req); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Invalid JSON body", nil)
		return
	}
	card, err := c.Svc.RecordAgentEvent(r.Context(), chi.URLParam(r, "cardId"), workboardsvc.AgentEventInput{
		Kind:    req.Kind,
		Payload: req.Payload,
	})
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, newWorkCardResponse(card))
}

func (c *WorkboardController) listGlobal(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, http.MethodGet, "/api/v1/workboard/cards")
		return
	}
	cards, err := c.Svc.ListAll(r.Context())
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, ListWorkCardsResponse{Cards: workCardResponses(cards)})
}

func (c *WorkboardController) createGlobal(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, http.MethodPost, "/api/v1/workboard/cards")
		return
	}
	var req CreateWorkCardRequest
	if err := decodeJSONStrict(r, &req); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Invalid JSON body", nil)
		return
	}
	card, err := c.Svc.Create(r.Context(), req.toInput(req.ProjectID))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusCreated, newWorkCardResponse(card))
}

func (c *WorkboardController) listRedo(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, http.MethodGet, "/api/v1/workboard/cards/{cardId}/redo")
		return
	}
	cardID := chi.URLParam(r, "cardId")
	cycles, err := c.Svc.ListRedo(r.Context(), cardID)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	respCycles := make([]RedoCycleResponse, 0, len(cycles))
	for _, cycle := range cycles {
		findingsResp := make([]RedoFindingResponse, 0, len(cycle.Findings))
		for _, f := range cycle.Findings {
			findingsResp = append(findingsResp, RedoFindingResponse{
				ID:           f.ID,
				CycleID:      f.CycleID,
				Sequence:     f.Sequence,
				Severity:     string(f.Severity),
				Title:        f.Title,
				Details:      f.Details,
				Command:      f.Command,
				ErrorOutput:  f.ErrorOutput,
				FileRefs:     f.FileRefs,
				Status:       string(f.Status),
				AttemptCount: f.AttemptCount,
				CreatedAt:    f.CreatedAt,
				UpdatedAt:    f.UpdatedAt,
			})
		}
		respCycles = append(respCycles, RedoCycleResponse{
			ID:          cycle.ID,
			CardID:      cycle.CardID,
			CycleNumber: cycle.CycleNumber,
			Source:      cycle.Source,
			Summary:     cycle.Summary,
			Findings:    findingsResp,
			CreatedAt:   cycle.CreatedAt,
			CompletedAt: cycle.CompletedAt,
		})
	}
	envelope.WriteJSON(w, http.StatusOK, ListRedoCyclesResponse{Cycles: respCycles})
}
