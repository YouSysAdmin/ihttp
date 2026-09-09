package project_test

import (
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/yousysadmin/ihttp/internal/domain/project"
	models "github.com/yousysadmin/ihttp/internal/models/project"
	"github.com/yousysadmin/ihttp/internal/testutil"
)

func TestLifecycle(t *testing.T) {
	stack := testutil.New(t)
	ctx := t.Context()
	svc := stack.Projects

	if _, err := svc.Create(ctx, "  "); !errors.Is(err, project.ErrInvalidName) {
		t.Fatalf("blank name: %v", err)
	}

	a, err := svc.Create(ctx, "Alpha")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Create(ctx, "alpha"); !errors.Is(err, project.ErrNameTaken) {
		t.Fatalf("duplicate name: %v", err)
	}

	if svc.Active() != nil {
		t.Fatal("nothing should be open yet")
	}

	if _, err := svc.Open(ctx, a.ID); err != nil {
		t.Fatal(err)
	}

	if svc.ActiveID() != a.ID {
		t.Fatal("open did not activate")
	}

	if err := svc.Delete(ctx, a.ID); !errors.Is(err, project.ErrActive) {
		t.Fatalf("deleting the open project: %v", err)
	}

	list, err := svc.List(ctx)
	if err != nil || len(list) != 1 || !list[0].IsActive {
		t.Fatalf("list: %+v %v", list, err)
	}

	_, err = svc.UpdateSettings(ctx, func(s *models.Settings) error {
		s.Scope = []models.ScopeRule{{URL: "["}}

		return nil
	})
	if err == nil {
		t.Fatal("a bad scope rule must be refused")
	}

	p, err := svc.UpdateSettings(ctx, func(s *models.Settings) error {
		s.Scope = []models.ScopeRule{{URL: `^https://example\.com/`}}
		s.Intercept.RequestFilter = `req.method = POST`

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(p.Settings.Scope) != 1 || svc.Active().InterceptRequest == nil {
		t.Fatalf("settings not applied: %+v", p.Settings)
	}

	// The open project is remembered across a restart.
	fresh := project.NewService(project.NewStore(stack.DB), stack.Bus, slog.New(slog.DiscardHandler))
	if err := fresh.Restore(ctx); err != nil || fresh.ActiveID() != a.ID {
		t.Fatalf("restore: active=%q err=%v", fresh.ActiveID(), err)
	}

	// Settings survive a reopen.
	svc.Close()

	if svc.Active() != nil {
		t.Fatal("close did not deactivate")
	}

	again, err := svc.Open(ctx, a.ID)
	if err != nil || again.Settings.Intercept.RequestFilter != `req.method = POST` {
		t.Fatalf("reopened: %+v %v", again.Settings, err)
	}

	svc.Close()

	if err := svc.Delete(ctx, a.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Get(ctx, a.ID); !errors.Is(err, project.ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
}

// A view is validated where every other setting is: a query that does
// not parse, a missing name or two views under one name are refused
// before they are stored, and the message names the offender.
func TestViewsAreValidated(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "views")
	ctx := t.Context()

	set := func(views []models.View) error {
		_, err := stack.Projects.UpdateSettings(ctx, func(s *models.Settings) error {
			s.Views = views

			return nil
		})

		return err
	}

	good := []models.View{
		{ID: "a", Name: "Errors", Query: "res.statusCode >= 400"},
		{ID: "b", Name: "Slow", Query: "res.ttfb > 1s", OnlyInScope: true},
		{ID: "c", Name: "Kept", Saved: true},
	}

	if err := set(good); err != nil {
		t.Fatalf("a good set of views was refused: %v", err)
	}

	if got := stack.Projects.Active().Project.Settings.Views; len(got) != 3 || got[1].Name != "Slow" {
		t.Fatalf("views were not stored: %+v", got)
	}

	bad := map[string][]models.View{
		"a query that does not parse": {{ID: "a", Name: "Broken", Query: "res.statusCode >= "}},
		"a field that does not exist": {{ID: "a", Name: "Nope", Query: "req.nothing = 1"}},
		"no name":                     {{ID: "a", Name: "  ", Query: ""}},
		"two by one name": {
			{ID: "a", Name: "Errors", Query: ""},
			{ID: "b", Name: "errors", Query: ""},
		},
	}

	for what, views := range bad {
		if err := set(views); err == nil {
			t.Errorf("%s was accepted", what)
		}
	}

	// A refusal leaves what was there alone.
	if got := stack.Projects.Active().Project.Settings.Views; len(got) != 3 {
		t.Fatalf("a refused update changed the stored views: %+v", got)
	}

	// More than the cap is refused, so a picker stays a picker.
	many := make([]models.View, models.MaxViews+1)
	for i := range many {
		many[i] = models.View{ID: fmt.Sprint(i), Name: fmt.Sprintf("v%d", i)}
	}

	if err := set(many); err == nil {
		t.Errorf("%d views were accepted, the cap is %d", len(many), models.MaxViews)
	}
}

// The entry cap is validated too: it is a guard, not an invitation to
// ask for a hundred million rows.
func TestEntryCapIsValidated(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "cap")
	ctx := t.Context()

	set := func(n int) error {
		_, err := stack.Projects.UpdateSettings(ctx, func(s *models.Settings) error {
			s.RequestLog.MaxEntries = n

			return nil
		})

		return err
	}

	for _, n := range []int{0, 1, 50_000, models.MaxEntriesLimit} {
		if err := set(n); err != nil {
			t.Errorf("a cap of %d was refused: %v", n, err)
		}
	}

	for _, n := range []int{-1, models.MaxEntriesLimit + 1} {
		if err := set(n); err == nil {
			t.Errorf("a cap of %d was accepted", n)
		}
	}
}
