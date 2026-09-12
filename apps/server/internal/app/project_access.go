package app

import (
	"context"
	"errors"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

const projectDirectoryLimit = 50

type ProjectDirectoryUser struct {
	ID          string
	Username    string
	Email       string
	DisplayName string
}

type ProjectDirectoryGroup struct {
	ID   string
	Name string
}

type ProjectAccessService struct {
	controlPlane *Service
	store        store.ProjectAccessStore
}

func NewProjectAccessService(controlPlane *Service, accessStore store.ProjectAccessStore) (*ProjectAccessService, error) {
	if controlPlane == nil {
		return nil, errors.New("control-plane service is required")
	}
	if accessStore == nil {
		return nil, errors.New("project access store is required")
	}
	return &ProjectAccessService{controlPlane: controlPlane, store: accessStore}, nil
}

func (s *ProjectAccessService) ListProjects(ctx context.Context, actor AuthenticatedUser) ([]store.Project, error) {
	if err := requireNormalAuthenticatedUser(actor); err != nil {
		return nil, err
	}
	if actor.DeploymentRole == store.DeploymentRoleAdmin {
		return s.controlPlane.ListProjects(ctx)
	}
	return s.store.ListProjectsForUser(ctx, actor.ID)
}

func (s *ProjectAccessService) CreateProject(ctx context.Context, actor AuthenticatedUser, input store.Project) (store.Project, error) {
	if err := requireNormalAuthenticatedUser(actor); err != nil {
		return store.Project{}, err
	}
	if actor.DeploymentRole != store.DeploymentRoleMember && actor.DeploymentRole != store.DeploymentRoleAdmin {
		return store.Project{}, NewError("forbidden", "project creation is not allowed", store.ErrInvalidArgument)
	}
	if err := validateProject(input); err != nil {
		return store.Project{}, err
	}
	prepared, err := s.controlPlane.ensureProjectRepository(ctx, input)
	if err != nil {
		return store.Project{}, err
	}
	project, err := s.store.CreateProjectWithAdmin(ctx, prepared, actor.ID)
	return project, translateStoreError(err, "project")
}

func (s *ProjectAccessService) EffectiveRole(ctx context.Context, actor AuthenticatedUser, projectID string) (string, error) {
	if err := requireNormalAuthenticatedUser(actor); err != nil {
		return "", err
	}
	if actor.DeploymentRole == store.DeploymentRoleAdmin {
		if _, err := s.controlPlane.GetProject(ctx, projectID); err != nil {
			return "", err
		}
		return store.ProjectRoleAdmin, nil
	}
	role, err := s.store.EffectiveProjectRole(ctx, projectID, actor.ID)
	if errors.Is(err, store.ErrNotFound) {
		return "", NewError("project_not_found", "project not found", err)
	}
	if err != nil {
		return "", err
	}
	return role, nil
}

func (s *ProjectAccessService) RequireRole(ctx context.Context, actor AuthenticatedUser, projectID, minimumRole string) (string, error) {
	if !store.ValidProjectRole(minimumRole) {
		return "", NewError("invalid_argument", "project role is invalid", store.ErrInvalidArgument)
	}
	role, err := s.EffectiveRole(ctx, actor, projectID)
	if err != nil {
		return "", err
	}
	if !store.ProjectRoleAtLeast(role, minimumRole) {
		return "", NewError("forbidden", "project role does not permit this operation", store.ErrInvalidArgument)
	}
	return role, nil
}

// AuthorizeRead is the shared application boundary for Project-scoped reads.
// Transports authenticate the actor, while this boundary owns the fixed
// viewer < member < admin policy.
func (s *ProjectAccessService) AuthorizeRead(ctx context.Context, actor AuthenticatedUser, projectID string) error {
	_, err := s.RequireRole(ctx, actor, projectID, store.ProjectRoleViewer)
	return err
}

// AuthorizeWorkflowMutation is the shared application boundary for normal
// Project workflow mutations such as Issue, Run, Question, and Review actions.
func (s *ProjectAccessService) AuthorizeWorkflowMutation(ctx context.Context, actor AuthenticatedUser, projectID string) error {
	_, err := s.RequireRole(ctx, actor, projectID, store.ProjectRoleMember)
	return err
}

// AuthorizeAdministration is the shared application boundary for Project
// configuration and Project access management.
func (s *ProjectAccessService) AuthorizeAdministration(ctx context.Context, actor AuthenticatedUser, projectID string) error {
	_, err := s.RequireRole(ctx, actor, projectID, store.ProjectRoleAdmin)
	return err
}

func (s *ProjectAccessService) GetProject(ctx context.Context, actor AuthenticatedUser, projectID string) (store.Project, string, error) {
	role, err := s.RequireRole(ctx, actor, projectID, store.ProjectRoleViewer)
	if err != nil {
		return store.Project{}, "", err
	}
	project, err := s.controlPlane.GetProject(ctx, projectID)
	return project, role, err
}

func (s *ProjectAccessService) UpdateProject(ctx context.Context, actor AuthenticatedUser, input store.Project) (store.Project, error) {
	if err := s.AuthorizeAdministration(ctx, actor, input.ID); err != nil {
		return store.Project{}, err
	}
	return s.controlPlane.UpdateProject(ctx, input)
}

// The methods below deliberately wrap the existing workflow services instead
// of reimplementing their lifecycles. They form the trusted application entry
// points for human Project operations used by HTTP and any future transport.

func (s *ProjectAccessService) ResolveIssueUUID(ctx context.Context, actor AuthenticatedUser, projectID, key string) (string, error) {
	if err := s.AuthorizeRead(ctx, actor, projectID); err != nil {
		return "", err
	}
	return s.controlPlane.ResolveIssueUUID(ctx, projectID, key)
}

func (s *ProjectAccessService) ListIssues(ctx context.Context, actor AuthenticatedUser, projectID string) ([]store.Issue, error) {
	if err := s.AuthorizeRead(ctx, actor, projectID); err != nil {
		return nil, err
	}
	return s.controlPlane.ListIssues(ctx, projectID)
}

func (s *ProjectAccessService) GetIssue(ctx context.Context, actor AuthenticatedUser, projectID, issueID string) (store.Issue, error) {
	if err := s.AuthorizeRead(ctx, actor, projectID); err != nil {
		return store.Issue{}, err
	}
	return s.controlPlane.GetIssue(ctx, projectID, issueID)
}

func (s *ProjectAccessService) CreateIssue(ctx context.Context, actor AuthenticatedUser, input store.Issue) (store.Issue, error) {
	if err := s.AuthorizeWorkflowMutation(ctx, actor, input.ProjectID); err != nil {
		return store.Issue{}, err
	}
	return s.controlPlane.CreateIssue(ctx, input)
}

func (s *ProjectAccessService) UpdateIssue(ctx context.Context, actor AuthenticatedUser, input store.Issue) (store.Issue, error) {
	if err := s.AuthorizeWorkflowMutation(ctx, actor, input.ProjectID); err != nil {
		return store.Issue{}, err
	}
	return s.controlPlane.UpdateIssue(ctx, input)
}

func (s *ProjectAccessService) AssignIssue(ctx context.Context, actor AuthenticatedUser, projectID, issueID, agentID string) (store.Issue, store.Run, error) {
	if err := s.AuthorizeWorkflowMutation(ctx, actor, projectID); err != nil {
		return store.Issue{}, store.Run{}, err
	}
	return s.controlPlane.AssignIssue(ctx, projectID, issueID, agentID)
}

func (s *ProjectAccessService) ListRuns(ctx context.Context, actor AuthenticatedUser, projectID string) ([]store.Run, error) {
	if err := s.AuthorizeRead(ctx, actor, projectID); err != nil {
		return nil, err
	}
	return s.controlPlane.ListRuns(ctx, projectID)
}

func (s *ProjectAccessService) GetRun(ctx context.Context, actor AuthenticatedUser, projectID, runID string) (store.Run, error) {
	if err := s.AuthorizeRead(ctx, actor, projectID); err != nil {
		return store.Run{}, err
	}
	return s.controlPlane.GetRun(ctx, projectID, runID)
}

func (s *ProjectAccessService) ListQuestions(ctx context.Context, actor AuthenticatedUser, questions *QuestionService, projectID string, filter store.QuestionFilter) ([]store.Question, error) {
	if err := s.AuthorizeRead(ctx, actor, projectID); err != nil {
		return nil, err
	}
	if questions == nil {
		return nil, errors.New("question service is unavailable")
	}
	return questions.List(ctx, projectID, filter)
}

func (s *ProjectAccessService) GetQuestion(ctx context.Context, actor AuthenticatedUser, questions *QuestionService, projectID, questionID string) (store.Question, error) {
	if err := s.AuthorizeRead(ctx, actor, projectID); err != nil {
		return store.Question{}, err
	}
	if questions == nil {
		return store.Question{}, errors.New("question service is unavailable")
	}
	return questions.Get(ctx, projectID, questionID)
}

func (s *ProjectAccessService) AnswerQuestion(ctx context.Context, actor AuthenticatedUser, questions *QuestionService, projectID, questionID string, answer store.QuestionAnswer) (store.AnswerQuestionResult, error) {
	if err := s.AuthorizeWorkflowMutation(ctx, actor, projectID); err != nil {
		return store.AnswerQuestionResult{}, err
	}
	if questions == nil {
		return store.AnswerQuestionResult{}, errors.New("question service is unavailable")
	}
	return questions.Answer(ctx, projectID, questionID, answer, nil)
}

func (s *ProjectAccessService) ListReviews(ctx context.Context, actor AuthenticatedUser, reviews *ReviewService, projectID string, filter store.ReviewFilter) ([]store.Review, error) {
	if err := s.AuthorizeRead(ctx, actor, projectID); err != nil {
		return nil, err
	}
	if reviews == nil {
		return nil, errors.New("review service is unavailable")
	}
	return reviews.List(ctx, projectID, filter)
}

func (s *ProjectAccessService) GetReview(ctx context.Context, actor AuthenticatedUser, reviews *ReviewService, projectID, reviewID string) (ReviewInspection, error) {
	if err := s.AuthorizeRead(ctx, actor, projectID); err != nil {
		return ReviewInspection{}, err
	}
	if reviews == nil {
		return ReviewInspection{}, errors.New("review service is unavailable")
	}
	return reviews.Get(ctx, projectID, reviewID)
}

func (s *ProjectAccessService) ApproveReview(ctx context.Context, actor AuthenticatedUser, reviews *ReviewService, projectID, reviewID string) (store.CompleteReviewApprovalResult, error) {
	if err := s.AuthorizeWorkflowMutation(ctx, actor, projectID); err != nil {
		return store.CompleteReviewApprovalResult{}, err
	}
	if reviews == nil {
		return store.CompleteReviewApprovalResult{}, errors.New("review service is unavailable")
	}
	return reviews.Approve(ctx, projectID, reviewID, nil)
}

func (s *ProjectAccessService) RequestReviewChanges(ctx context.Context, actor AuthenticatedUser, reviews *ReviewService, projectID, reviewID, feedback string) (store.RequestReviewChangesResult, error) {
	if err := s.AuthorizeWorkflowMutation(ctx, actor, projectID); err != nil {
		return store.RequestReviewChangesResult{}, err
	}
	if reviews == nil {
		return store.RequestReviewChangesResult{}, errors.New("review service is unavailable")
	}
	return reviews.RequestChanges(ctx, projectID, reviewID, feedback, nil)
}

func (s *ProjectAccessService) ListUserAccess(ctx context.Context, actor AuthenticatedUser, projectID string) ([]store.ProjectUserAccessView, error) {
	if err := s.AuthorizeAdministration(ctx, actor, projectID); err != nil {
		return nil, err
	}
	values, err := s.store.ListProjectUserAccess(ctx, projectID)
	return values, projectAccessStoreError(err)
}

func (s *ProjectAccessService) UpsertUserAccess(ctx context.Context, actor AuthenticatedUser, input store.ProjectUserAccess) (store.ProjectUserAccess, error) {
	if err := s.AuthorizeAdministration(ctx, actor, input.ProjectID); err != nil {
		return store.ProjectUserAccess{}, err
	}
	if !store.ValidProjectRole(input.Role) {
		return store.ProjectUserAccess{}, NewError("invalid_argument", "project role must be admin, member, or viewer", store.ErrInvalidArgument)
	}
	if _, err := s.store.GetProjectAccessUser(ctx, input.UserID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.ProjectUserAccess{}, NewError("user_not_found", "user not found", err)
		}
		return store.ProjectUserAccess{}, err
	}
	value, err := s.store.UpsertProjectUserAccess(ctx, input)
	return value, projectAccessStoreError(err)
}

func (s *ProjectAccessService) DeleteUserAccess(ctx context.Context, actor AuthenticatedUser, projectID, userID string) error {
	if err := s.AuthorizeAdministration(ctx, actor, projectID); err != nil {
		return err
	}
	return projectAccessStoreError(s.store.DeleteProjectUserAccess(ctx, projectID, userID))
}

func (s *ProjectAccessService) ListGroupAccess(ctx context.Context, actor AuthenticatedUser, projectID string) ([]store.ProjectGroupAccessView, error) {
	if err := s.AuthorizeAdministration(ctx, actor, projectID); err != nil {
		return nil, err
	}
	values, err := s.store.ListProjectGroupAccess(ctx, projectID)
	return values, projectAccessStoreError(err)
}

func (s *ProjectAccessService) UpsertGroupAccess(ctx context.Context, actor AuthenticatedUser, input store.ProjectGroupAccess) (store.ProjectGroupAccess, error) {
	if err := s.AuthorizeAdministration(ctx, actor, input.ProjectID); err != nil {
		return store.ProjectGroupAccess{}, err
	}
	if !store.ValidProjectRole(input.Role) {
		return store.ProjectGroupAccess{}, NewError("invalid_argument", "project role must be admin, member, or viewer", store.ErrInvalidArgument)
	}
	groups, err := s.store.ListProjectAccessGroups(ctx)
	if err != nil {
		return store.ProjectGroupAccess{}, err
	}
	found := false
	for _, group := range groups {
		if group.ID == input.GroupID {
			found = true
			break
		}
	}
	if !found {
		return store.ProjectGroupAccess{}, NewError("group_not_found", "group not found", store.ErrNotFound)
	}
	value, err := s.store.UpsertProjectGroupAccess(ctx, input)
	return value, projectAccessStoreError(err)
}

func (s *ProjectAccessService) DeleteGroupAccess(ctx context.Context, actor AuthenticatedUser, projectID, groupID string) error {
	if err := s.AuthorizeAdministration(ctx, actor, projectID); err != nil {
		return err
	}
	return projectAccessStoreError(s.store.DeleteProjectGroupAccess(ctx, projectID, groupID))
}

func (s *ProjectAccessService) SearchUsers(ctx context.Context, actor AuthenticatedUser, projectID, query string) ([]ProjectDirectoryUser, error) {
	if err := s.AuthorizeAdministration(ctx, actor, projectID); err != nil {
		return nil, err
	}
	users, err := s.store.ListProjectAccessUsers(ctx)
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	result := make([]ProjectDirectoryUser, 0)
	for _, user := range users {
		if user.Status != store.UserStatusActive {
			continue
		}
		if query != "" && !containsFold(user.Username, query) && !containsFold(user.Email, query) && !containsFold(user.DisplayName, query) {
			continue
		}
		result = append(result, ProjectDirectoryUser{ID: user.ID, Username: user.Username, Email: user.Email, DisplayName: user.DisplayName})
		if len(result) == projectDirectoryLimit {
			break
		}
	}
	return result, nil
}

func (s *ProjectAccessService) SearchGroups(ctx context.Context, actor AuthenticatedUser, projectID, query string) ([]ProjectDirectoryGroup, error) {
	if err := s.AuthorizeAdministration(ctx, actor, projectID); err != nil {
		return nil, err
	}
	groups, err := s.store.ListProjectAccessGroups(ctx)
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	result := make([]ProjectDirectoryGroup, 0)
	for _, group := range groups {
		if query != "" && !containsFold(group.Name, query) {
			continue
		}
		result = append(result, ProjectDirectoryGroup{ID: group.ID, Name: group.Name})
		if len(result) == projectDirectoryLimit {
			break
		}
	}
	return result, nil
}

func containsFold(value, lowerQuery string) bool {
	return strings.Contains(strings.ToLower(value), lowerQuery)
}

func projectAccessStoreError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrLastProjectAdmin):
		return NewError("last_project_admin", "every project must retain an active direct user admin", err)
	case errors.Is(err, store.ErrNotFound):
		return NewError("project_access_not_found", "project access grant not found", err)
	case errors.Is(err, store.ErrInvalidArgument):
		return NewError("invalid_argument", "invalid project access request", err)
	case errors.Is(err, store.ErrConflict):
		return NewError("conflict", "project access conflicts with existing state", err)
	default:
		return err
	}
}
