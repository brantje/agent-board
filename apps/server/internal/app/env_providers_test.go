package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/secrets"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type envProviderStore struct {
	store.ControlPlaneStore
	providers                 []store.Provider
	modelProfiles             []store.ModelProfile
	created                   store.Provider
	updated                   store.Provider
	createdModels             []store.ModelProfile
	createErr                 error
	createModelErr            error
	updateErr                 error
	putErr                    error
	listCalls                 int
	deferProvidersUntilRelist bool
	nextModelProfileID        int
}

func (s *envProviderStore) ListProviders(_ context.Context, _ *string) ([]store.Provider, error) {
	s.listCalls++
	if s.deferProvidersUntilRelist && s.listCalls == 1 {
		return nil, nil
	}
	out := append([]store.Provider(nil), s.providers...)
	if s.created.ID != "" {
		found := false
		for _, provider := range out {
			if provider.ID == s.created.ID {
				found = true
				break
			}
		}
		if !found {
			out = append(out, s.created)
		}
	}
	return out, nil
}

func (s *envProviderStore) GetProvider(_ context.Context, _ *string, id string) (store.Provider, error) {
	for _, provider := range s.providers {
		if provider.ID == id {
			return provider, nil
		}
	}
	if s.created.ID == id {
		return s.created, nil
	}
	return store.Provider{}, store.ErrNotFound
}

func (s *envProviderStore) CreateProvider(_ context.Context, p store.Provider) (store.Provider, error) {
	if s.createErr != nil {
		return store.Provider{}, s.createErr
	}
	p.ID = strings.ToLower(strings.ReplaceAll(p.Name, " ", "-")) + "-provider-id"
	p.HealthStatus = "UNKNOWN"
	if p.SafeMetadata == nil {
		p.SafeMetadata = store.EmptyObject
	}
	s.created = p
	return p, nil
}

func (s *envProviderStore) UpdateProvider(_ context.Context, _ *string, p store.Provider) (store.Provider, error) {
	if s.updateErr != nil {
		return store.Provider{}, s.updateErr
	}
	s.updated = p
	if s.created.ID == p.ID {
		s.created = p
	}
	for i, provider := range s.providers {
		if provider.ID == p.ID {
			s.providers[i] = p
		}
	}
	return p, nil
}

func (s *envProviderStore) ListModelProfiles(_ context.Context, projectID *string) ([]store.ModelProfile, error) {
	if projectID != nil {
		return nil, nil
	}
	return append([]store.ModelProfile(nil), s.modelProfiles...), nil
}

func (s *envProviderStore) CreateModelProfile(_ context.Context, p store.ModelProfile) (store.ModelProfile, error) {
	if s.createModelErr != nil {
		return store.ModelProfile{}, s.createModelErr
	}
	s.nextModelProfileID++
	p.ID = "model-profile-" + strings.TrimSpace(p.Name)
	if p.GenerationSettings == nil {
		p.GenerationSettings = store.EmptyObject
	}
	s.createdModels = append(s.createdModels, p)
	return p, nil
}

type captureEnvSecretWriter struct {
	ref        string
	value      []byte
	calls      int
	err        error
	resolveErr error
	stored     map[string][]byte
}

func newCaptureEnvSecretWriter() *captureEnvSecretWriter {
	return &captureEnvSecretWriter{stored: make(map[string][]byte)}
}

func (w *captureEnvSecretWriter) Put(_ context.Context, _ secrets.Scope, ref string, value []byte) (secrets.Metadata, error) {
	w.calls++
	w.ref = ref
	w.value = append([]byte(nil), value...)
	if w.stored != nil {
		w.stored[ref] = append([]byte(nil), value...)
	}
	if w.err != nil {
		return secrets.Metadata{}, w.err
	}
	return secrets.Metadata{Ref: ref}, nil
}

func (w *captureEnvSecretWriter) Resolve(_ context.Context, _ secrets.Scope, ref string) ([]byte, error) {
	if w.resolveErr != nil {
		return nil, w.resolveErr
	}
	if w.stored == nil {
		return nil, secrets.ErrNotFound
	}
	value, ok := w.stored[ref]
	if !ok {
		return nil, secrets.ErrNotFound
	}
	return append([]byte(nil), value...), nil
}

func envGetter(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}

func TestParseCommaSeparatedEnv(t *testing.T) {
	got := parseCommaSeparatedEnv(" model-a , ,model-b ")
	if len(got) != 2 || got[0] != "model-a" || got[1] != "model-b" {
		t.Fatalf("parseCommaSeparatedEnv() = %#v", got)
	}
	if parseCommaSeparatedEnv("   ") != nil {
		t.Fatal("expected nil for blank input")
	}
}

func TestDeriveOpenRouterModelProfileName(t *testing.T) {
	if got := deriveOpenRouterModelProfileName("anthropic/claude-3.5-sonnet"); got != "claude-3.5-sonnet" {
		t.Fatalf("derived name = %q", got)
	}
	if got := deriveOpenRouterModelProfileName("gpt-4o-mini"); got != "gpt-4o-mini" {
		t.Fatalf("derived name = %q", got)
	}
}

func TestEnsureOpenRouterFromEnvNoOpWhenUnset(t *testing.T) {
	st := &envProviderStore{}
	writer := newCaptureEnvSecretWriter()
	svc := New(st)

	if err := EnsureOpenRouterFromEnv(context.Background(), svc, writer, envGetter(nil)); err != nil {
		t.Fatalf("EnsureOpenRouterFromEnv() error = %v", err)
	}
	if st.created.ID != "" {
		t.Fatal("expected no provider create")
	}
	if len(st.createdModels) != 0 {
		t.Fatal("expected no model profile create")
	}
	if writer.calls != 0 {
		t.Fatalf("secret writes = %d, want 0", writer.calls)
	}
}

func TestEnsureOpenRouterFromEnvNoOpWhenBlank(t *testing.T) {
	st := &envProviderStore{}
	writer := newCaptureEnvSecretWriter()
	svc := New(st)

	if err := EnsureOpenRouterFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
		openRouterEnvKey:       "   ",
		openRouterModelsEnvKey: " , ",
	})); err != nil {
		t.Fatalf("EnsureOpenRouterFromEnv() error = %v", err)
	}
	if st.created.ID != "" {
		t.Fatal("expected no provider create")
	}
	if len(st.createdModels) != 0 {
		t.Fatal("expected no model profile create")
	}
	if writer.calls != 0 {
		t.Fatalf("secret writes = %d, want 0", writer.calls)
	}
}

func TestEnsureOpenRouterFromEnvCreatesProviderWithCredential(t *testing.T) {
	st := &envProviderStore{}
	writer := newCaptureEnvSecretWriter()
	svc := New(st)

	if err := EnsureOpenRouterFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
		openRouterEnvKey: "sk-openrouter-key",
	})); err != nil {
		t.Fatalf("EnsureOpenRouterFromEnv() error = %v", err)
	}
	if st.created.Name != "OpenRouter" || st.created.Kind != "openrouter" {
		t.Fatalf("created provider = %+v", st.created)
	}
	if !st.created.Enabled {
		t.Fatal("expected created provider to be enabled")
	}
	if writer.calls != 1 {
		t.Fatalf("secret writes = %d, want 1", writer.calls)
	}
	if string(writer.value) != "sk-openrouter-key" {
		t.Fatalf("stored credential = %q", writer.value)
	}
	expectedRef := "provider:openrouter-provider-id"
	if writer.ref != expectedRef {
		t.Fatalf("secret ref = %q, want %q", writer.ref, expectedRef)
	}
	if st.updated.CredentialRef == nil || *st.updated.CredentialRef != expectedRef {
		t.Fatalf("updated credential ref = %v", st.updated.CredentialRef)
	}
}

func TestEnsureOpenRouterFromEnvSkipsExistingProviderCaseInsensitive(t *testing.T) {
	credentialRef := "provider:existing"
	for _, name := range []string{"OpenRouter", "openrouter", "OPENROUTER"} {
		t.Run(name, func(t *testing.T) {
			st := &envProviderStore{providers: []store.Provider{{ID: "existing", Name: name, Kind: "openrouter", CredentialRef: &credentialRef}}}
			writer := newCaptureEnvSecretWriter()
			writer.stored[credentialRef] = []byte("existing-key")
			svc := New(st)

			if err := EnsureOpenRouterFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
				openRouterEnvKey: "sk-openrouter-key",
			})); err != nil {
				t.Fatalf("EnsureOpenRouterFromEnv() error = %v", err)
			}
			if st.created.ID != "" {
				t.Fatal("expected no provider create")
			}
			if writer.calls != 0 {
				t.Fatalf("secret writes = %d, want 0", writer.calls)
			}
		})
	}
}

func TestEnsureOpenRouterFromEnvResumesCredentialForProviderWithoutCredentialRef(t *testing.T) {
	st := &envProviderStore{providers: []store.Provider{{ID: "existing", Name: "OpenRouter", Kind: "openrouter"}}}
	writer := newCaptureEnvSecretWriter()
	svc := New(st)

	if err := EnsureOpenRouterFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
		openRouterEnvKey: "sk-openrouter-key",
	})); err != nil {
		t.Fatalf("EnsureOpenRouterFromEnv() error = %v", err)
	}
	if writer.calls != 1 {
		t.Fatalf("secret writes = %d, want 1", writer.calls)
	}
	if st.updated.CredentialRef == nil || *st.updated.CredentialRef != "provider:existing" {
		t.Fatalf("updated credential ref = %v", st.updated.CredentialRef)
	}
}

func TestEnsureOpenRouterFromEnvCreateConflictReloadsProvider(t *testing.T) {
	st := &envProviderStore{
		createErr:                 store.ErrConflict,
		deferProvidersUntilRelist: true,
		providers:                 []store.Provider{{ID: "existing-from-race", Name: "OpenRouter", Kind: "openrouter"}},
	}
	writer := newCaptureEnvSecretWriter()
	svc := New(st)

	if err := EnsureOpenRouterFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
		openRouterEnvKey:       "sk-openrouter-key",
		openRouterModelsEnvKey: "openai/gpt-4o-mini",
	})); err != nil {
		t.Fatalf("EnsureOpenRouterFromEnv() error = %v", err)
	}
	if st.listCalls < 2 {
		t.Fatalf("list calls = %d, want at least 2", st.listCalls)
	}
	if writer.calls != 1 {
		t.Fatalf("secret writes = %d, want 1", writer.calls)
	}
	if len(st.createdModels) != 1 {
		t.Fatalf("created models = %d, want 1", len(st.createdModels))
	}
}

func TestEnsureOpenRouterFromEnvPropagatesCredentialStoreFailure(t *testing.T) {
	st := &envProviderStore{}
	writer := &captureEnvSecretWriter{err: errors.New("secret store unavailable")}
	svc := New(st)

	err := EnsureOpenRouterFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
		openRouterEnvKey: "sk-openrouter-key",
	}))
	if err == nil {
		t.Fatal("expected error")
	}
	if st.updated.ID != "" {
		t.Fatal("expected provider credential ref not to be updated after secret failure")
	}
}

func TestEnsureOpenRouterFromEnvResumesAfterUpdateProviderFailure(t *testing.T) {
	st := &envProviderStore{
		providers: []store.Provider{{ID: "existing", Name: "OpenRouter", Kind: "openrouter"}},
		updateErr: errors.New("database unavailable"),
	}
	writer := newCaptureEnvSecretWriter()
	svc := New(st)

	err := EnsureOpenRouterFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
		openRouterEnvKey: "sk-openrouter-key",
	}))
	if err == nil {
		t.Fatal("expected error")
	}
	if writer.calls != 1 {
		t.Fatalf("secret writes = %d, want 1", writer.calls)
	}

	st.updateErr = nil
	writer.calls = 0
	if err := EnsureOpenRouterFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
		openRouterEnvKey: "sk-openrouter-key",
	})); err != nil {
		t.Fatalf("EnsureOpenRouterFromEnv() retry error = %v", err)
	}
	if writer.calls != 1 {
		t.Fatalf("retry secret writes = %d, want 1", writer.calls)
	}
	if st.updated.CredentialRef == nil || *st.updated.CredentialRef != "provider:existing" {
		t.Fatalf("updated credential ref = %v", st.updated.CredentialRef)
	}
}

func TestEnsureOpenRouterFromEnvRequiresSecretStorage(t *testing.T) {
	st := &envProviderStore{}
	svc := New(st)

	err := EnsureOpenRouterFromEnv(context.Background(), svc, nil, envGetter(map[string]string{
		openRouterEnvKey: "sk-openrouter-key",
	}))
	if err == nil {
		t.Fatal("expected error when secret storage is unavailable")
	}
	if st.created.ID != "" {
		t.Fatal("expected no provider create")
	}
}

func TestEnsureOpenRouterFromEnvPropagatesCreateErrors(t *testing.T) {
	st := &envProviderStore{createErr: errors.New("database unavailable")}
	writer := newCaptureEnvSecretWriter()
	svc := New(st)

	if err := EnsureOpenRouterFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
		openRouterEnvKey: "sk-openrouter-key",
	})); err == nil {
		t.Fatal("expected error")
	}
	if writer.calls != 0 {
		t.Fatalf("secret writes = %d, want 0", writer.calls)
	}
}

func TestEnsureOpenRouterFromEnvCreatesModelProfiles(t *testing.T) {
	st := &envProviderStore{
		providers: []store.Provider{{ID: "openrouter-provider-id", Name: "OpenRouter", Kind: "openrouter"}},
	}
	writer := newCaptureEnvSecretWriter()
	svc := New(st)

	if err := EnsureOpenRouterFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
		openRouterModelsEnvKey: "anthropic/claude-3.5-sonnet, openai/gpt-4o-mini",
	})); err != nil {
		t.Fatalf("EnsureOpenRouterFromEnv() error = %v", err)
	}
	if len(st.createdModels) != 2 {
		t.Fatalf("created models = %d, want 2", len(st.createdModels))
	}
	if st.createdModels[0].Name != "claude-3.5-sonnet" || st.createdModels[0].Model != "anthropic/claude-3.5-sonnet" {
		t.Fatalf("first model profile = %+v", st.createdModels[0])
	}
	if st.createdModels[0].ProjectID != nil {
		t.Fatal("expected global model profile")
	}
	if st.createdModels[0].ProviderID != "openrouter-provider-id" {
		t.Fatalf("provider id = %q", st.createdModels[0].ProviderID)
	}
	if st.createdModels[1].Name != "gpt-4o-mini" || st.createdModels[1].Model != "openai/gpt-4o-mini" {
		t.Fatalf("second model profile = %+v", st.createdModels[1])
	}
}

func TestEnsureOpenRouterFromEnvSkipsExistingModelProfileByName(t *testing.T) {
	st := &envProviderStore{
		providers: []store.Provider{{ID: "openrouter-provider-id", Name: "OpenRouter", Kind: "openrouter"}},
		modelProfiles: []store.ModelProfile{
			{ID: "existing", Name: "claude-3.5-sonnet", Model: "other/model", ProviderID: "other-provider"},
		},
	}
	svc := New(st)

	if err := EnsureOpenRouterFromEnv(context.Background(), svc, nil, envGetter(map[string]string{
		openRouterModelsEnvKey: "anthropic/claude-3.5-sonnet",
	})); err != nil {
		t.Fatalf("EnsureOpenRouterFromEnv() error = %v", err)
	}
	if len(st.createdModels) != 0 {
		t.Fatalf("created models = %d, want 0", len(st.createdModels))
	}
}

func TestEnsureOpenRouterFromEnvSkipsExistingModelProfileByModel(t *testing.T) {
	st := &envProviderStore{
		providers: []store.Provider{{ID: "openrouter-provider-id", Name: "OpenRouter", Kind: "openrouter"}},
		modelProfiles: []store.ModelProfile{
			{ID: "existing", Name: "Different Name", Model: "anthropic/claude-3.5-sonnet", ProviderID: "openrouter-provider-id"},
		},
	}
	svc := New(st)

	if err := EnsureOpenRouterFromEnv(context.Background(), svc, nil, envGetter(map[string]string{
		openRouterModelsEnvKey: "anthropic/claude-3.5-sonnet",
	})); err != nil {
		t.Fatalf("EnsureOpenRouterFromEnv() error = %v", err)
	}
	if len(st.createdModels) != 0 {
		t.Fatalf("created models = %d, want 0", len(st.createdModels))
	}
}

func TestEnsureOpenRouterFromEnvSkipsModelCreateOnConflict(t *testing.T) {
	st := &envProviderStore{
		providers:      []store.Provider{{ID: "openrouter-provider-id", Name: "OpenRouter", Kind: "openrouter"}},
		createModelErr: store.ErrConflict,
	}
	svc := New(st)

	if err := EnsureOpenRouterFromEnv(context.Background(), svc, nil, envGetter(map[string]string{
		openRouterModelsEnvKey: "anthropic/claude-3.5-sonnet",
	})); err != nil {
		t.Fatalf("EnsureOpenRouterFromEnv() error = %v", err)
	}
}

func TestEnsureOpenRouterFromEnvModelsWithoutProviderFails(t *testing.T) {
	st := &envProviderStore{}
	svc := New(st)

	err := EnsureOpenRouterFromEnv(context.Background(), svc, nil, envGetter(map[string]string{
		openRouterModelsEnvKey: "anthropic/claude-3.5-sonnet",
	}))
	if err == nil {
		t.Fatal("expected error when OpenRouter provider does not exist")
	}
}

func TestEnsureOpenRouterFromEnvModelsWithExistingProvider(t *testing.T) {
	st := &envProviderStore{
		providers: []store.Provider{{ID: "openrouter-provider-id", Name: "OpenRouter", Kind: "openrouter"}},
	}
	svc := New(st)

	if err := EnsureOpenRouterFromEnv(context.Background(), svc, nil, envGetter(map[string]string{
		openRouterModelsEnvKey: "gpt-4o-mini",
	})); err != nil {
		t.Fatalf("EnsureOpenRouterFromEnv() error = %v", err)
	}
	if len(st.createdModels) != 1 {
		t.Fatalf("created models = %d, want 1", len(st.createdModels))
	}
	if st.createdModels[0].Name != "gpt-4o-mini" || st.createdModels[0].Model != "gpt-4o-mini" {
		t.Fatalf("created model profile = %+v", st.createdModels[0])
	}
}

func TestEnsureOpenRouterFromEnvExistingProviderWithAPIKeyAndModels(t *testing.T) {
	credentialRef := "provider:openrouter-provider-id"
	st := &envProviderStore{
		providers: []store.Provider{{ID: "openrouter-provider-id", Name: "OpenRouter", Kind: "openrouter", CredentialRef: &credentialRef}},
	}
	writer := newCaptureEnvSecretWriter()
	writer.stored[credentialRef] = []byte("existing-key")
	svc := New(st)

	if err := EnsureOpenRouterFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
		openRouterEnvKey:       "sk-openrouter-key",
		openRouterModelsEnvKey: "openai/gpt-4o-mini",
	})); err != nil {
		t.Fatalf("EnsureOpenRouterFromEnv() error = %v", err)
	}
	if st.created.ID != "" {
		t.Fatal("expected no provider create")
	}
	if writer.calls != 0 {
		t.Fatalf("secret writes = %d, want 0", writer.calls)
	}
	if len(st.createdModels) != 1 {
		t.Fatalf("created models = %d, want 1", len(st.createdModels))
	}
}

func TestEnsureOpenRouterFromEnvFailsWhenProviderNameMatchesWrongKind(t *testing.T) {
	st := &envProviderStore{providers: []store.Provider{{ID: "custom", Name: "OpenRouter", Kind: "openai-compatible"}}}
	writer := newCaptureEnvSecretWriter()
	svc := New(st)

	err := EnsureOpenRouterFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
		openRouterEnvKey: "sk-openrouter-key",
	}))
	if err == nil {
		t.Fatal("expected error for wrong provider kind")
	}
	if writer.calls != 0 {
		t.Fatalf("secret writes = %d, want 0", writer.calls)
	}
}

func TestEnsureOpenRouterFromEnvFailsWhenProviderKindHasWrongCasing(t *testing.T) {
	st := &envProviderStore{providers: []store.Provider{{ID: "custom", Name: "OpenRouter", Kind: "OpenRouter"}}}
	writer := newCaptureEnvSecretWriter()
	svc := New(st)

	err := EnsureOpenRouterFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
		openRouterEnvKey: "sk-openrouter-key",
	}))
	if err == nil {
		t.Fatal("expected error for non-canonical provider kind casing")
	}
	if writer.calls != 0 {
		t.Fatalf("secret writes = %d, want 0", writer.calls)
	}
}

func TestEnsureOpenRouterFromEnvResumesWhenCredentialRefDangling(t *testing.T) {
	credentialRef := "provider:existing"
	st := &envProviderStore{providers: []store.Provider{{ID: "existing", Name: "OpenRouter", Kind: "openrouter", CredentialRef: &credentialRef}}}
	writer := newCaptureEnvSecretWriter()
	svc := New(st)

	if err := EnsureOpenRouterFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
		openRouterEnvKey: "sk-openrouter-key",
	})); err != nil {
		t.Fatalf("EnsureOpenRouterFromEnv() error = %v", err)
	}
	if writer.calls != 1 {
		t.Fatalf("secret writes = %d, want 1", writer.calls)
	}
}

func TestEnsureOpenRouterFromEnvPropagatesCredentialResolveFailure(t *testing.T) {
	credentialRef := "provider:existing"
	st := &envProviderStore{providers: []store.Provider{{ID: "existing", Name: "OpenRouter", Kind: "openrouter", CredentialRef: &credentialRef}}}
	writer := newCaptureEnvSecretWriter()
	writer.resolveErr = errors.New("secret store unavailable")
	svc := New(st)

	err := EnsureOpenRouterFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
		openRouterEnvKey: "sk-openrouter-key",
	}))
	if err == nil {
		t.Fatal("expected error")
	}
	if writer.calls != 0 {
		t.Fatalf("secret writes = %d, want 0", writer.calls)
	}
}

func TestEnsureLiteLLMFromEnvNoOpWhenUnset(t *testing.T) {
	st := &envProviderStore{}
	writer := newCaptureEnvSecretWriter()
	svc := New(st)

	if err := EnsureLiteLLMFromEnv(context.Background(), svc, writer, nil); err != nil {
		t.Fatalf("EnsureLiteLLMFromEnv() error = %v", err)
	}
	if st.created.ID != "" {
		t.Fatal("expected no provider create")
	}
	if writer.calls != 0 {
		t.Fatalf("secret writes = %d, want 0", writer.calls)
	}
}

func TestEnsureLiteLLMFromEnvRequiresControlPlane(t *testing.T) {
	if err := EnsureLiteLLMFromEnv(context.Background(), nil, nil, envGetter(map[string]string{
		liteLLMEndpointEnvKey: "https://litellm.example/v1",
		liteLLMAPIKeyEnvKey:   "sk-litellm-key",
	})); err == nil {
		t.Fatal("expected error when control plane is missing")
	}
}

func TestEnsureLiteLLMFromEnvNoOpWhenBlank(t *testing.T) {
	st := &envProviderStore{}
	writer := newCaptureEnvSecretWriter()
	svc := New(st)

	if err := EnsureLiteLLMFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
		liteLLMEndpointEnvKey: "   ",
		liteLLMAPIKeyEnvKey:   "  ",
		liteLLMModelsEnvKey:   " , ",
	})); err != nil {
		t.Fatalf("EnsureLiteLLMFromEnv() error = %v", err)
	}
	if st.created.ID != "" {
		t.Fatal("expected no provider create")
	}
	if writer.calls != 0 {
		t.Fatalf("secret writes = %d, want 0", writer.calls)
	}
}

func TestEnsureLiteLLMFromEnvCreatesProviderWithEndpointAndCredential(t *testing.T) {
	st := &envProviderStore{}
	writer := newCaptureEnvSecretWriter()
	svc := New(st)

	if err := EnsureLiteLLMFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
		liteLLMEndpointEnvKey: "https://litellm.example/v1",
		liteLLMAPIKeyEnvKey:   "sk-litellm-key",
	})); err != nil {
		t.Fatalf("EnsureLiteLLMFromEnv() error = %v", err)
	}
	if st.created.Name != "LiteLLM" || st.created.Kind != "openai-compatible" {
		t.Fatalf("created provider = %+v", st.created)
	}
	if !st.created.Enabled {
		t.Fatal("expected created provider to be enabled")
	}
	if st.created.BaseURL == nil || *st.created.BaseURL != "https://litellm.example/v1" {
		t.Fatalf("created base URL = %v", st.created.BaseURL)
	}
	if writer.calls != 1 {
		t.Fatalf("secret writes = %d, want 1", writer.calls)
	}
	if string(writer.value) != "sk-litellm-key" {
		t.Fatalf("stored credential = %q", writer.value)
	}
	expectedRef := "provider:litellm-provider-id"
	if writer.ref != expectedRef {
		t.Fatalf("secret ref = %q, want %q", writer.ref, expectedRef)
	}
	if st.updated.CredentialRef == nil || *st.updated.CredentialRef != expectedRef {
		t.Fatalf("updated credential ref = %v", st.updated.CredentialRef)
	}
}

func TestEnsureLiteLLMFromEnvRequiresEndpointAndAPIKeyToCreate(t *testing.T) {
	t.Run("endpoint only", func(t *testing.T) {
		st := &envProviderStore{}
		writer := newCaptureEnvSecretWriter()
		svc := New(st)
		err := EnsureLiteLLMFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
			liteLLMEndpointEnvKey: "https://litellm.example/v1",
		}))
		if err == nil {
			t.Fatal("expected error when API key is missing")
		}
		if st.created.ID != "" {
			t.Fatal("expected no provider create")
		}
		if writer.calls != 0 {
			t.Fatalf("secret writes = %d, want 0", writer.calls)
		}
	})
	t.Run("api key only", func(t *testing.T) {
		st := &envProviderStore{}
		writer := newCaptureEnvSecretWriter()
		svc := New(st)
		err := EnsureLiteLLMFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
			liteLLMAPIKeyEnvKey: "sk-litellm-key",
		}))
		if err == nil {
			t.Fatal("expected error when endpoint is missing")
		}
		if st.created.ID != "" {
			t.Fatal("expected no provider create")
		}
		if writer.calls != 0 {
			t.Fatalf("secret writes = %d, want 0", writer.calls)
		}
	})
}

func TestEnsureLiteLLMFromEnvSkipsExistingProviderCaseInsensitive(t *testing.T) {
	credentialRef := "provider:existing"
	baseURL := "https://existing.example/v1"
	for _, name := range []string{"LiteLLM", "litellm", "LITELLM"} {
		t.Run(name, func(t *testing.T) {
			st := &envProviderStore{providers: []store.Provider{{ID: "existing", Name: name, Kind: "openai-compatible", BaseURL: &baseURL, CredentialRef: &credentialRef}}}
			writer := newCaptureEnvSecretWriter()
			writer.stored[credentialRef] = []byte("existing-key")
			svc := New(st)

			if err := EnsureLiteLLMFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
				liteLLMEndpointEnvKey: "https://litellm.example/v1",
				liteLLMAPIKeyEnvKey:   "sk-litellm-key",
			})); err != nil {
				t.Fatalf("EnsureLiteLLMFromEnv() error = %v", err)
			}
			if st.created.ID != "" {
				t.Fatal("expected no provider create")
			}
			if writer.calls != 0 {
				t.Fatalf("secret writes = %d, want 0", writer.calls)
			}
			if st.updated.ID != "" {
				t.Fatal("expected existing provider not to be updated")
			}
		})
	}
}

func TestEnsureLiteLLMFromEnvResumesCredentialForProviderWithoutCredentialRef(t *testing.T) {
	baseURL := "https://existing.example/v1"
	st := &envProviderStore{providers: []store.Provider{{ID: "existing", Name: "LiteLLM", Kind: "openai-compatible", BaseURL: &baseURL}}}
	writer := newCaptureEnvSecretWriter()
	svc := New(st)

	if err := EnsureLiteLLMFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
		liteLLMEndpointEnvKey: "https://litellm.example/v1",
		liteLLMAPIKeyEnvKey:   "sk-litellm-key",
	})); err != nil {
		t.Fatalf("EnsureLiteLLMFromEnv() error = %v", err)
	}
	if writer.calls != 1 {
		t.Fatalf("secret writes = %d, want 1", writer.calls)
	}
	if st.updated.CredentialRef == nil || *st.updated.CredentialRef != "provider:existing" {
		t.Fatalf("updated credential ref = %v", st.updated.CredentialRef)
	}
	if st.updated.BaseURL == nil || *st.updated.BaseURL != baseURL {
		t.Fatalf("base URL overwritten = %v", st.updated.BaseURL)
	}
}

func TestEnsureLiteLLMFromEnvResumesBaseURLWhenMissing(t *testing.T) {
	credentialRef := "provider:existing"
	st := &envProviderStore{providers: []store.Provider{{ID: "existing", Name: "LiteLLM", Kind: "openai-compatible", CredentialRef: &credentialRef}}}
	writer := newCaptureEnvSecretWriter()
	writer.stored[credentialRef] = []byte("existing-key")
	svc := New(st)

	if err := EnsureLiteLLMFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
		liteLLMEndpointEnvKey: "https://litellm.example/v1",
		liteLLMAPIKeyEnvKey:   "sk-litellm-key",
	})); err != nil {
		t.Fatalf("EnsureLiteLLMFromEnv() error = %v", err)
	}
	if writer.calls != 0 {
		t.Fatalf("secret writes = %d, want 0", writer.calls)
	}
	if st.updated.BaseURL == nil || *st.updated.BaseURL != "https://litellm.example/v1" {
		t.Fatalf("updated base URL = %v", st.updated.BaseURL)
	}
}

func TestEnsureLiteLLMFromEnvCreateConflictReloadsProvider(t *testing.T) {
	st := &envProviderStore{
		createErr:                 store.ErrConflict,
		deferProvidersUntilRelist: true,
		providers:                 []store.Provider{{ID: "existing-from-race", Name: "LiteLLM", Kind: "openai-compatible"}},
	}
	writer := newCaptureEnvSecretWriter()
	svc := New(st)

	if err := EnsureLiteLLMFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
		liteLLMEndpointEnvKey: "https://litellm.example/v1",
		liteLLMAPIKeyEnvKey:   "sk-litellm-key",
	})); err != nil {
		t.Fatalf("EnsureLiteLLMFromEnv() error = %v", err)
	}
	if st.listCalls < 2 {
		t.Fatalf("list calls = %d, want at least 2", st.listCalls)
	}
	if writer.calls != 1 {
		t.Fatalf("secret writes = %d, want 1", writer.calls)
	}
	if st.updated.BaseURL == nil || *st.updated.BaseURL != "https://litellm.example/v1" {
		t.Fatalf("updated base URL = %v", st.updated.BaseURL)
	}
}

func TestEnsureLiteLLMFromEnvPropagatesCredentialStoreFailure(t *testing.T) {
	st := &envProviderStore{}
	writer := &captureEnvSecretWriter{err: errors.New("secret store unavailable")}
	svc := New(st)

	err := EnsureLiteLLMFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
		liteLLMEndpointEnvKey: "https://litellm.example/v1",
		liteLLMAPIKeyEnvKey:   "sk-litellm-key",
	}))
	if err == nil {
		t.Fatal("expected error")
	}
	if st.updated.ID != "" {
		t.Fatal("expected provider credential ref not to be updated after secret failure")
	}
}

func TestEnsureLiteLLMFromEnvResumesAfterUpdateProviderFailure(t *testing.T) {
	st := &envProviderStore{
		providers: []store.Provider{{ID: "existing", Name: "LiteLLM", Kind: "openai-compatible"}},
		updateErr: errors.New("database unavailable"),
	}
	writer := newCaptureEnvSecretWriter()
	svc := New(st)

	err := EnsureLiteLLMFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
		liteLLMEndpointEnvKey: "https://litellm.example/v1",
		liteLLMAPIKeyEnvKey:   "sk-litellm-key",
	}))
	if err == nil {
		t.Fatal("expected error")
	}
	if writer.calls != 1 {
		t.Fatalf("secret writes = %d, want 1", writer.calls)
	}

	st.updateErr = nil
	writer.calls = 0
	if err := EnsureLiteLLMFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
		liteLLMEndpointEnvKey: "https://litellm.example/v1",
		liteLLMAPIKeyEnvKey:   "sk-litellm-key",
	})); err != nil {
		t.Fatalf("EnsureLiteLLMFromEnv() retry error = %v", err)
	}
	if writer.calls != 1 {
		t.Fatalf("retry secret writes = %d, want 1", writer.calls)
	}
	if st.updated.CredentialRef == nil || *st.updated.CredentialRef != "provider:existing" {
		t.Fatalf("updated credential ref = %v", st.updated.CredentialRef)
	}
	if st.updated.BaseURL == nil || *st.updated.BaseURL != "https://litellm.example/v1" {
		t.Fatalf("updated base URL = %v", st.updated.BaseURL)
	}
}

func TestEnsureLiteLLMFromEnvRequiresSecretStorage(t *testing.T) {
	st := &envProviderStore{}
	svc := New(st)

	err := EnsureLiteLLMFromEnv(context.Background(), svc, nil, envGetter(map[string]string{
		liteLLMEndpointEnvKey: "https://litellm.example/v1",
		liteLLMAPIKeyEnvKey:   "sk-litellm-key",
	}))
	if err == nil {
		t.Fatal("expected error when secret storage is unavailable")
	}
	if st.created.ID != "" {
		t.Fatal("expected no provider create")
	}
}

func TestEnsureLiteLLMFromEnvPropagatesCreateErrors(t *testing.T) {
	st := &envProviderStore{createErr: errors.New("database unavailable")}
	writer := newCaptureEnvSecretWriter()
	svc := New(st)

	if err := EnsureLiteLLMFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
		liteLLMEndpointEnvKey: "https://litellm.example/v1",
		liteLLMAPIKeyEnvKey:   "sk-litellm-key",
	})); err == nil {
		t.Fatal("expected error")
	}
	if writer.calls != 0 {
		t.Fatalf("secret writes = %d, want 0", writer.calls)
	}
}

func TestEnsureLiteLLMFromEnvFailsWhenProviderNameMatchesWrongKind(t *testing.T) {
	st := &envProviderStore{providers: []store.Provider{{ID: "custom", Name: "LiteLLM", Kind: "openrouter"}}}
	writer := newCaptureEnvSecretWriter()
	svc := New(st)

	err := EnsureLiteLLMFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
		liteLLMEndpointEnvKey: "https://litellm.example/v1",
		liteLLMAPIKeyEnvKey:   "sk-litellm-key",
	}))
	if err == nil {
		t.Fatal("expected error for wrong provider kind")
	}
	if writer.calls != 0 {
		t.Fatalf("secret writes = %d, want 0", writer.calls)
	}
}

func TestEnsureLiteLLMFromEnvFailsWhenProviderKindHasWrongCasing(t *testing.T) {
	st := &envProviderStore{providers: []store.Provider{{ID: "custom", Name: "LiteLLM", Kind: "OpenAI-Compatible"}}}
	writer := newCaptureEnvSecretWriter()
	svc := New(st)

	err := EnsureLiteLLMFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
		liteLLMEndpointEnvKey: "https://litellm.example/v1",
		liteLLMAPIKeyEnvKey:   "sk-litellm-key",
	}))
	if err == nil {
		t.Fatal("expected error for non-canonical provider kind casing")
	}
	if writer.calls != 0 {
		t.Fatalf("secret writes = %d, want 0", writer.calls)
	}
}

func TestEnsureLiteLLMFromEnvResumesWhenCredentialRefDangling(t *testing.T) {
	credentialRef := "provider:existing"
	baseURL := "https://existing.example/v1"
	st := &envProviderStore{providers: []store.Provider{{ID: "existing", Name: "LiteLLM", Kind: "openai-compatible", BaseURL: &baseURL, CredentialRef: &credentialRef}}}
	writer := newCaptureEnvSecretWriter()
	svc := New(st)

	if err := EnsureLiteLLMFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
		liteLLMEndpointEnvKey: "https://litellm.example/v1",
		liteLLMAPIKeyEnvKey:   "sk-litellm-key",
	})); err != nil {
		t.Fatalf("EnsureLiteLLMFromEnv() error = %v", err)
	}
	if writer.calls != 1 {
		t.Fatalf("secret writes = %d, want 1", writer.calls)
	}
}

func TestEnsureLiteLLMFromEnvPropagatesCredentialResolveFailure(t *testing.T) {
	credentialRef := "provider:existing"
	st := &envProviderStore{providers: []store.Provider{{ID: "existing", Name: "LiteLLM", Kind: "openai-compatible", CredentialRef: &credentialRef}}}
	writer := newCaptureEnvSecretWriter()
	writer.resolveErr = errors.New("secret store unavailable")
	svc := New(st)

	err := EnsureLiteLLMFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
		liteLLMEndpointEnvKey: "https://litellm.example/v1",
		liteLLMAPIKeyEnvKey:   "sk-litellm-key",
	}))
	if err == nil {
		t.Fatal("expected error")
	}
	if writer.calls != 0 {
		t.Fatalf("secret writes = %d, want 0", writer.calls)
	}
}

func TestEnsureLiteLLMFromEnvPartialEnvWithExistingProvider(t *testing.T) {
	credentialRef := "provider:existing"
	baseURL := "https://existing.example/v1"
	st := &envProviderStore{providers: []store.Provider{{ID: "existing", Name: "LiteLLM", Kind: "openai-compatible", BaseURL: &baseURL, CredentialRef: &credentialRef}}}
	writer := newCaptureEnvSecretWriter()
	writer.stored[credentialRef] = []byte("existing-key")
	svc := New(st)

	if err := EnsureLiteLLMFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
		liteLLMEndpointEnvKey: "https://litellm.example/v1",
	})); err != nil {
		t.Fatalf("EnsureLiteLLMFromEnv() endpoint-only error = %v", err)
	}
	if err := EnsureLiteLLMFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
		liteLLMAPIKeyEnvKey: "sk-litellm-key",
	})); err != nil {
		t.Fatalf("EnsureLiteLLMFromEnv() api-key-only error = %v", err)
	}
	if st.created.ID != "" || st.updated.ID != "" {
		t.Fatal("expected existing complete provider not to be changed")
	}
	if writer.calls != 0 {
		t.Fatalf("secret writes = %d, want 0", writer.calls)
	}
}

func TestEnsureLiteLLMFromEnvResumeRequiresSecretStorage(t *testing.T) {
	st := &envProviderStore{providers: []store.Provider{{ID: "existing", Name: "LiteLLM", Kind: "openai-compatible"}}}
	svc := New(st)

	err := EnsureLiteLLMFromEnv(context.Background(), svc, nil, envGetter(map[string]string{
		liteLLMAPIKeyEnvKey: "sk-litellm-key",
	}))
	if err == nil {
		t.Fatal("expected error when secret storage is unavailable")
	}
}

func TestEnsureLiteLLMFromEnvCreatesModelProfiles(t *testing.T) {
	st := &envProviderStore{
		providers: []store.Provider{{ID: "litellm-provider-id", Name: "LiteLLM", Kind: "openai-compatible"}},
	}
	writer := newCaptureEnvSecretWriter()
	svc := New(st)

	if err := EnsureLiteLLMFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
		liteLLMModelsEnvKey: "anthropic/claude-3.5-sonnet, openai/gpt-4o-mini",
	})); err != nil {
		t.Fatalf("EnsureLiteLLMFromEnv() error = %v", err)
	}
	if len(st.createdModels) != 2 {
		t.Fatalf("created models = %d, want 2", len(st.createdModels))
	}
	if st.createdModels[0].Name != "claude-3.5-sonnet" || st.createdModels[0].Model != "anthropic/claude-3.5-sonnet" {
		t.Fatalf("first model profile = %+v", st.createdModels[0])
	}
	if st.createdModels[0].ProjectID != nil {
		t.Fatal("expected global model profile")
	}
	if st.createdModels[0].ProviderID != "litellm-provider-id" {
		t.Fatalf("provider id = %q", st.createdModels[0].ProviderID)
	}
	if st.createdModels[1].Name != "gpt-4o-mini" || st.createdModels[1].Model != "openai/gpt-4o-mini" {
		t.Fatalf("second model profile = %+v", st.createdModels[1])
	}
}

func TestEnsureLiteLLMFromEnvSkipsExistingModelProfileByName(t *testing.T) {
	st := &envProviderStore{
		providers: []store.Provider{{ID: "litellm-provider-id", Name: "LiteLLM", Kind: "openai-compatible"}},
		modelProfiles: []store.ModelProfile{
			{ID: "existing", Name: "claude-3.5-sonnet", Model: "other/model", ProviderID: "other-provider"},
		},
	}
	svc := New(st)

	if err := EnsureLiteLLMFromEnv(context.Background(), svc, nil, envGetter(map[string]string{
		liteLLMModelsEnvKey: "anthropic/claude-3.5-sonnet",
	})); err != nil {
		t.Fatalf("EnsureLiteLLMFromEnv() error = %v", err)
	}
	if len(st.createdModels) != 0 {
		t.Fatalf("created models = %d, want 0", len(st.createdModels))
	}
}

func TestEnsureLiteLLMFromEnvSkipsExistingModelProfileByModel(t *testing.T) {
	st := &envProviderStore{
		providers: []store.Provider{{ID: "litellm-provider-id", Name: "LiteLLM", Kind: "openai-compatible"}},
		modelProfiles: []store.ModelProfile{
			{ID: "existing", Name: "Different Name", Model: "anthropic/claude-3.5-sonnet", ProviderID: "litellm-provider-id"},
		},
	}
	svc := New(st)

	if err := EnsureLiteLLMFromEnv(context.Background(), svc, nil, envGetter(map[string]string{
		liteLLMModelsEnvKey: "anthropic/claude-3.5-sonnet",
	})); err != nil {
		t.Fatalf("EnsureLiteLLMFromEnv() error = %v", err)
	}
	if len(st.createdModels) != 0 {
		t.Fatalf("created models = %d, want 0", len(st.createdModels))
	}
}

func TestEnsureLiteLLMFromEnvSkipsModelCreateOnConflict(t *testing.T) {
	st := &envProviderStore{
		providers:      []store.Provider{{ID: "litellm-provider-id", Name: "LiteLLM", Kind: "openai-compatible"}},
		createModelErr: store.ErrConflict,
	}
	svc := New(st)

	if err := EnsureLiteLLMFromEnv(context.Background(), svc, nil, envGetter(map[string]string{
		liteLLMModelsEnvKey: "anthropic/claude-3.5-sonnet",
	})); err != nil {
		t.Fatalf("EnsureLiteLLMFromEnv() error = %v", err)
	}
}

func TestEnsureLiteLLMFromEnvModelsWithoutProviderFails(t *testing.T) {
	st := &envProviderStore{}
	svc := New(st)

	err := EnsureLiteLLMFromEnv(context.Background(), svc, nil, envGetter(map[string]string{
		liteLLMModelsEnvKey: "anthropic/claude-3.5-sonnet",
	}))
	if err == nil {
		t.Fatal("expected error when LiteLLM provider does not exist")
	}
}

func TestEnsureLiteLLMFromEnvModelsWithExistingProvider(t *testing.T) {
	st := &envProviderStore{
		providers: []store.Provider{{ID: "litellm-provider-id", Name: "LiteLLM", Kind: "openai-compatible"}},
	}
	svc := New(st)

	if err := EnsureLiteLLMFromEnv(context.Background(), svc, nil, envGetter(map[string]string{
		liteLLMModelsEnvKey: "gpt-4o-mini",
	})); err != nil {
		t.Fatalf("EnsureLiteLLMFromEnv() error = %v", err)
	}
	if len(st.createdModels) != 1 {
		t.Fatalf("created models = %d, want 1", len(st.createdModels))
	}
	if st.createdModels[0].Name != "gpt-4o-mini" || st.createdModels[0].Model != "gpt-4o-mini" {
		t.Fatalf("created model profile = %+v", st.createdModels[0])
	}
}

func TestEnsureLiteLLMFromEnvExistingProviderWithAPIKeyAndModels(t *testing.T) {
	credentialRef := "provider:litellm-provider-id"
	baseURL := "https://existing.example/v1"
	st := &envProviderStore{
		providers: []store.Provider{{ID: "litellm-provider-id", Name: "LiteLLM", Kind: "openai-compatible", BaseURL: &baseURL, CredentialRef: &credentialRef}},
	}
	writer := newCaptureEnvSecretWriter()
	writer.stored[credentialRef] = []byte("existing-key")
	svc := New(st)

	if err := EnsureLiteLLMFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
		liteLLMEndpointEnvKey: "https://litellm.example/v1",
		liteLLMAPIKeyEnvKey:   "sk-litellm-key",
		liteLLMModelsEnvKey:   "openai/gpt-4o-mini",
	})); err != nil {
		t.Fatalf("EnsureLiteLLMFromEnv() error = %v", err)
	}
	if st.created.ID != "" {
		t.Fatal("expected no provider create")
	}
	if writer.calls != 0 {
		t.Fatalf("secret writes = %d, want 0", writer.calls)
	}
	if len(st.createdModels) != 1 {
		t.Fatalf("created models = %d, want 1", len(st.createdModels))
	}
}

func TestEnsureLiteLLMFromEnvCreateConflictReloadsProviderAndModels(t *testing.T) {
	st := &envProviderStore{
		createErr:                 store.ErrConflict,
		deferProvidersUntilRelist: true,
		providers:                 []store.Provider{{ID: "existing-from-race", Name: "LiteLLM", Kind: "openai-compatible"}},
	}
	writer := newCaptureEnvSecretWriter()
	svc := New(st)

	if err := EnsureLiteLLMFromEnv(context.Background(), svc, writer, envGetter(map[string]string{
		liteLLMEndpointEnvKey: "https://litellm.example/v1",
		liteLLMAPIKeyEnvKey:   "sk-litellm-key",
		liteLLMModelsEnvKey:   "openai/gpt-4o-mini",
	})); err != nil {
		t.Fatalf("EnsureLiteLLMFromEnv() error = %v", err)
	}
	if st.listCalls < 2 {
		t.Fatalf("list calls = %d, want at least 2", st.listCalls)
	}
	if writer.calls != 1 {
		t.Fatalf("secret writes = %d, want 1", writer.calls)
	}
	if len(st.createdModels) != 1 {
		t.Fatalf("created models = %d, want 1", len(st.createdModels))
	}
}
