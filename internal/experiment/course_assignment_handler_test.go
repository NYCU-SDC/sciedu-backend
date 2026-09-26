package experiment

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sciedu-backend/internal/auth"

	middlewareutil "github.com/NYCU-SDC/summer/pkg/middleware"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandlerCourseOperations(t *testing.T) {
	experimentID := uuid.New()
	courseID := uuid.New()
	actorID := uuid.New()
	now := time.Now().UTC()
	assignment := CourseAssignment{
		Course: AssignedCourse{
			ID: courseID, Code: "BIO101", Title: "Biology", Status: CourseStatusPUBLISHED,
			CreatedAt: now, UpdatedAt: now,
		},
		LinkedAt: now,
	}
	removeCalls := 0
	service := &fakeHandlerService{
		listCoursesFn: func(_ context.Context, gotActorID, gotExperimentID uuid.UUID, page, pageSize int32) (CourseAssignmentPage, error) {
			assert.Equal(t, actorID, gotActorID)
			assert.Equal(t, experimentID, gotExperimentID)
			assert.Equal(t, int32(2), page)
			assert.Equal(t, int32(1), pageSize)
			return CourseAssignmentPage{Items: []CourseAssignment{assignment}, TotalItems: 2, TotalPages: 2, CurrentPage: page, PageSize: pageSize}, nil
		},
		addCoursesFn: func(_ context.Context, gotExperimentID uuid.UUID, courseIDs []uuid.UUID) ([]CourseAssignment, error) {
			assert.Equal(t, experimentID, gotExperimentID)
			assert.Equal(t, []uuid.UUID{courseID}, courseIDs)
			return []CourseAssignment{assignment}, nil
		},
		removeCourseFn: func(_ context.Context, gotExperimentID, gotCourseID uuid.UUID) error {
			assert.Equal(t, experimentID, gotExperimentID)
			assert.Equal(t, courseID, gotCourseID)
			removeCalls++
			return nil
		},
	}
	mux := directMux(NewHandler(service, nil), actorID)

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/experiments/"+experimentID.String()+"/courses?page=2&pageSize=1", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	var page paginatedExperimentCoursesResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &page))
	require.Len(t, page.Items, 1)
	assert.Equal(t, courseID, page.Items[0].Course.ID)
	assert.Equal(t, CourseStatusPUBLISHED, page.Items[0].Course.Status)
	assert.Equal(t, int32(2), page.TotalItems)

	recorder = httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/experiments/"+experimentID.String()+"/courses", strings.NewReader(`{"courseIds":["`+courseID.String()+`"]}`)))
	require.Equal(t, http.StatusOK, recorder.Code)
	var added []experimentCourseAssignmentResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &added))
	require.Len(t, added, 1)
	assert.Equal(t, courseID, added[0].Course.ID)

	recorder = httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, "/api/experiments/"+experimentID.String()+"/courses/"+courseID.String(), nil))
	assert.Equal(t, http.StatusNoContent, recorder.Code)
	assert.Equal(t, 1, removeCalls)
}

func TestHandlerCourseValidationAndConflict(t *testing.T) {
	experimentID := uuid.New()
	courseID := uuid.New()
	conflictService := &fakeHandlerService{removeCourseFn: func(context.Context, uuid.UUID, uuid.UUID) error {
		return fmt.Errorf("%w: ACTIVE experiments do not allow assignment removal", errExperimentConflict)
	}}
	tests := []struct {
		name     string
		service  *fakeHandlerService
		method   string
		path     string
		body     string
		wantCode int
	}{
		{name: "invalid list pagination", service: &fakeHandlerService{}, method: http.MethodGet, path: "/api/experiments/" + experimentID.String() + "/courses?page=0", wantCode: http.StatusBadRequest},
		{name: "invalid experiment id", service: &fakeHandlerService{}, method: http.MethodGet, path: "/api/experiments/not-a-uuid/courses", wantCode: http.StatusBadRequest},
		{name: "empty add batch", service: &fakeHandlerService{}, method: http.MethodPost, path: "/api/experiments/" + experimentID.String() + "/courses", body: `{"courseIds":[]}`, wantCode: http.StatusBadRequest},
		{name: "malformed add body", service: &fakeHandlerService{}, method: http.MethodPost, path: "/api/experiments/" + experimentID.String() + "/courses", body: `{`, wantCode: http.StatusBadRequest},
		{name: "invalid delete course id", service: &fakeHandlerService{}, method: http.MethodDelete, path: "/api/experiments/" + experimentID.String() + "/courses/not-a-uuid", wantCode: http.StatusBadRequest},
		{name: "delete lifecycle conflict", service: conflictService, method: http.MethodDelete, path: "/api/experiments/" + experimentID.String() + "/courses/" + courseID.String(), wantCode: http.StatusConflict},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := directMux(NewHandler(tt.service, nil), uuid.New())
			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body)))
			assert.Equal(t, tt.wantCode, recorder.Code)
		})
	}
}

func TestCourseRoutesAuthorization(t *testing.T) {
	experimentID := uuid.New()
	courseID := uuid.New()
	tests := []struct {
		name     string
		roles    []auth.Role
		method   string
		path     string
		body     string
		wantCode int
	}{
		{name: "student can list", roles: []auth.Role{auth.STUDENT}, method: http.MethodGet, path: "/api/experiments/" + experimentID.String() + "/courses", wantCode: http.StatusOK},
		{name: "student cannot add", roles: []auth.Role{auth.STUDENT}, method: http.MethodPost, path: "/api/experiments/" + experimentID.String() + "/courses", body: `{"courseIds":["` + courseID.String() + `"]}`, wantCode: http.StatusForbidden},
		{name: "student cannot remove", roles: []auth.Role{auth.STUDENT}, method: http.MethodDelete, path: "/api/experiments/" + experimentID.String() + "/courses/" + courseID.String(), wantCode: http.StatusForbidden},
		{name: "experimenter can add", roles: []auth.Role{auth.EXPERIMENTER}, method: http.MethodPost, path: "/api/experiments/" + experimentID.String() + "/courses", body: `{"courseIds":["` + courseID.String() + `"]}`, wantCode: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			handler := NewHandler(&fakeHandlerService{}, nil)
			handler.RegisterRoutes(mux, middlewareutil.NewSet(), auth.NewAuthorizer(fakeRoleQuerier{roles: tt.roles}, nil))
			request := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			request = request.WithContext(auth.ContextWithUserID(request.Context(), uuid.New()))
			recorder := httptest.NewRecorder()

			mux.ServeHTTP(recorder, request)

			assert.Equal(t, tt.wantCode, recorder.Code)
		})
	}
}

func TestHandlerListCoursesRequiresActor(t *testing.T) {
	handler := NewHandler(&fakeHandlerService{}, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/experiments/"+uuid.NewString()+"/courses", nil)
	request.SetPathValue("id", uuid.NewString())
	recorder := httptest.NewRecorder()

	handler.ListCourses(recorder, request)

	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}
