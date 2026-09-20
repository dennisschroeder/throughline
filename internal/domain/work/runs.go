package work

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ObjectiveMode says how an objective ends, not what phase it is in. A finite
// objective is finished once its goal is reached; an ongoing one keeps
// operating and is never closed by a Plan Run ending. Mode carries no cadence
// or scheduling meaning, and it is fixed at creation: changing it later would
// retroactively rewrite what every recorded Run meant.
type ObjectiveMode string

const (
	ObjectiveFinite  ObjectiveMode = "finite"
	ObjectiveOngoing ObjectiveMode = "ongoing"
)

func ValidObjectiveMode(value ObjectiveMode) bool {
	return oneOf(value, ObjectiveFinite, ObjectiveOngoing)
}

// PlanRunStatus is the whole terminal answer, not a lifecycle plus a separate
// outcome: finished only ever meant succeeded or failed, so one field carries
// both. cancelled asserts no outcome — the Run was abandoned; failed says it
// was carried to judgement and missed its obligations.
type PlanRunStatus string

const (
	PlanRunActive    PlanRunStatus = "active"
	PlanRunSucceeded PlanRunStatus = "succeeded"
	PlanRunFailed    PlanRunStatus = "failed"
	PlanRunCancelled PlanRunStatus = "cancelled"
)

// Terminal reports whether the Run has ended. Terminal Runs are irreversible;
// they are never reopened or reset for a repetition.
func (s PlanRunStatus) Terminal() bool {
	return s == PlanRunSucceeded || s == PlanRunFailed || s == PlanRunCancelled
}

func ValidPlanRunStatus(value PlanRunStatus) bool {
	return oneOf(value, PlanRunActive, PlanRunSucceeded, PlanRunFailed, PlanRunCancelled)
}

// WorkItemOrigin says where a work item came from, which decides whether it
// may execute at all.
type WorkItemOrigin string

const (
	// OriginLegacy marks execution that existed before Plan Runs. It has no
	// Run and keeps exactly the behavior it had; it is the sole historical
	// exception to the rule that execution belongs to a Run.
	OriginLegacy WorkItemOrigin = "legacy"
	// OriginPlanStep marks an item materialized from a Plan Step when its Run
	// was created. It executes only while that Run is active.
	OriginPlanStep WorkItemOrigin = "plan_step"
	// OriginUnplanned marks work proposed outside any Run. It records an idea
	// and is never executable; to act on it, take it into a Run.
	OriginUnplanned WorkItemOrigin = "unplanned"
)

func ValidWorkItemOrigin(value WorkItemOrigin) bool {
	return oneOf(value, OriginLegacy, OriginPlanStep, OriginUnplanned)
}

// PlanInput is a named value a Plan declares it needs. The Plan names it; a
// Run binds it. Throughline never computes one — in particular it derives no
// date, period or schedule of its own.
type PlanInput struct {
	ID          string
	PlanID      string
	Name        string
	Description string
	Required    bool
	Ordinal     int
	CreatedAt   time.Time
}

func NewPlanInput(input PlanInput, now time.Time) (PlanInput, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.PlanID = strings.TrimSpace(input.PlanID)
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	if input.ID == "" || input.PlanID == "" || input.Name == "" {
		return PlanInput{}, errors.New("plan input requires id, plan id, and name")
	}
	if input.Ordinal < 1 {
		return PlanInput{}, errors.New("plan input ordinal must be positive")
	}
	input.CreatedAt = now.UTC()
	return input, nil
}

// PlanStep is one reusable unit of a Plan definition. It is not executable and
// never becomes executable: creating a Run materializes a fresh WorkItem from
// it, and the Step itself is untouched by everything that then happens.
//
// Definition holds the step's owned child data — capabilities, acceptance
// criteria, expected outputs, output requirements and external action
// proposals. It is a frozen snapshot copied per Run, never queried across
// steps, so it lives as one immutable record rather than five tables whose
// rows nothing else references.
type PlanStep struct {
	ID                 string
	PlanID             string
	ClientRef          string
	ParentStepID       string
	Key                string
	Title              string
	Description        string
	Kind               string
	Required           bool
	Priority           Priority
	EstimatedScope     EstimatedScope
	ExecutionPolicy    ExecutionPolicy
	RequiredActorKind  ActorKind
	ReviewRequirements []ReviewRequirement
	Ordinal            int
	Definition         StepDefinition
	CreatedAt          time.Time
}

// StepDefinition is the part of a Plan Step copied verbatim into each Run's
// work item. Every field is data the Step declares; none of it is resolved,
// interpolated or looked up when a Run is created.
type StepDefinition struct {
	RequiredCapabilities []string                `json:"required_capabilities,omitempty"`
	AcceptanceCriteria   []StepCriterion         `json:"acceptance_criteria,omitempty"`
	ExpectedOutputs      []StepExpectedOutput    `json:"expected_outputs,omitempty"`
	OutputRequirements   []StepOutputRequirement `json:"output_requirements,omitempty"`
	ExternalActions      []StepExternalAction    `json:"external_actions,omitempty"`
}

type StepCriterion struct {
	Text     string `json:"text"`
	Required bool   `json:"required"`
	Ordinal  int    `json:"ordinal"`
}

type StepExpectedOutput struct {
	Name            string `json:"name"`
	ProfileName     string `json:"profile_name"`
	ProfileVersion  int    `json:"profile_version"`
	Contract        string `json:"contract,omitempty"`
	DestinationHint string `json:"destination_hint,omitempty"`
	Required        bool   `json:"required"`
	Ordinal         int    `json:"ordinal"`
}

// StepOutputRequirement names one exact accepted output revision a Run's work
// item needs. A profile-and-constraint requirement is deliberately not
// expressible here: resolving one at Run creation would silently pick up
// whatever some other Run happened to accept, which is exactly the implicit
// cross-Run selection the model forbids.
type StepOutputRequirement struct {
	RequiredOutputRevisionID string `json:"required_output_revision_id"`
	Required                 bool   `json:"required"`
	Note                     string `json:"note,omitempty"`
}

// StepExternalAction carries an authorization subject that is copied byte for
// byte into every Run. Throughline substitutes nothing into it, so a subject
// recorded here authorizes exactly what it says in every Run that copies it.
type StepExternalAction struct {
	Required             bool   `json:"required"`
	Title                string `json:"title"`
	Rationale            string `json:"rationale,omitempty"`
	AuthorizationSubject string `json:"authorization_subject"`
}

func NewPlanStep(step PlanStep, now time.Time) (PlanStep, error) {
	step.ID = strings.TrimSpace(step.ID)
	step.PlanID = strings.TrimSpace(step.PlanID)
	step.ClientRef = strings.TrimSpace(step.ClientRef)
	step.ParentStepID = strings.TrimSpace(step.ParentStepID)
	step.Key = strings.TrimSpace(step.Key)
	step.Title = strings.TrimSpace(step.Title)
	step.Description = strings.TrimSpace(step.Description)
	step.Kind = strings.TrimSpace(step.Kind)
	if step.ID == "" || step.PlanID == "" || step.ClientRef == "" || step.Key == "" || step.Title == "" {
		return PlanStep{}, errors.New("plan step requires id, plan id, client reference, key, and title")
	}
	if step.Kind == "" {
		return PlanStep{}, errors.New("plan step kind is required")
	}
	if step.Ordinal < 1 {
		return PlanStep{}, errors.New("plan step ordinal must be positive")
	}
	if strings.Contains(step.Key, RunKeySeparator) {
		return PlanStep{}, fmt.Errorf("plan step key %q must not contain %q, which separates it from the run sequence", step.Key, RunKeySeparator)
	}
	if !validPriority(step.Priority) {
		return PlanStep{}, fmt.Errorf("plan step: invalid priority %q", step.Priority)
	}
	if !validEstimatedScope(step.EstimatedScope) {
		return PlanStep{}, fmt.Errorf("plan step: invalid estimated scope %q", step.EstimatedScope)
	}
	if !validExecutionPolicy(step.ExecutionPolicy) {
		return PlanStep{}, fmt.Errorf("plan step: invalid execution policy %q", step.ExecutionPolicy)
	}
	if !validActorKind(step.RequiredActorKind) {
		return PlanStep{}, fmt.Errorf("plan step: invalid required actor kind %q", step.RequiredActorKind)
	}
	requirements, err := NormalizeReviewRequirements(step.ReviewRequirements)
	if err != nil {
		return PlanStep{}, err
	}
	step.ReviewRequirements = requirements
	if err := step.Definition.validate(); err != nil {
		return PlanStep{}, err
	}
	step.CreatedAt = now.UTC()
	return step, nil
}

func (d StepDefinition) validate() error {
	for _, capability := range d.RequiredCapabilities {
		if strings.TrimSpace(capability) == "" {
			return errors.New("plan step required capability cannot be empty")
		}
	}
	// Ordinals are unique within the step, because a run materializes them
	// into one work item where the same uniqueness is enforced. A duplicate
	// accepted here would approve an immutable definition that no run could
	// ever instantiate.
	criterionOrdinals := map[int]bool{}
	for _, criterion := range d.AcceptanceCriteria {
		if strings.TrimSpace(criterion.Text) == "" {
			return errors.New("plan step acceptance criterion requires text")
		}
		if criterion.Ordinal < 1 {
			return errors.New("plan step acceptance criterion ordinal must be positive")
		}
		if criterionOrdinals[criterion.Ordinal] {
			return fmt.Errorf("plan step declares acceptance criterion ordinal %d twice", criterion.Ordinal)
		}
		criterionOrdinals[criterion.Ordinal] = true
	}
	outputOrdinals := map[int]bool{}
	outputNames := map[string]bool{}
	for _, expected := range d.ExpectedOutputs {
		name := strings.TrimSpace(expected.Name)
		if name == "" || strings.TrimSpace(expected.ProfileName) == "" {
			return errors.New("plan step expected output requires a name and a profile")
		}
		if expected.ProfileVersion < 1 {
			return errors.New("plan step expected output requires an exact profile version")
		}
		if expected.Ordinal < 1 {
			return errors.New("plan step expected output ordinal must be positive")
		}
		if outputOrdinals[expected.Ordinal] {
			return fmt.Errorf("plan step declares expected output ordinal %d twice", expected.Ordinal)
		}
		if outputNames[name] {
			return fmt.Errorf("plan step declares expected output %q twice", name)
		}
		outputOrdinals[expected.Ordinal] = true
		outputNames[name] = true
	}
	for _, requirement := range d.OutputRequirements {
		if strings.TrimSpace(requirement.RequiredOutputRevisionID) == "" {
			return errors.New("plan step output requirement must name one exact accepted output revision; a profile constraint would resolve implicitly across runs")
		}
	}
	for _, action := range d.ExternalActions {
		if strings.TrimSpace(action.Title) == "" {
			return errors.New("plan step external action requires a title")
		}
		if strings.TrimSpace(action.AuthorizationSubject) == "" {
			return errors.New("plan step external action requires an authorization subject")
		}
	}
	return nil
}

// PlanStepDependency is a hard prerequisite between two steps of one Plan. It
// is copied into every Run as an ordinary dependency between that Run's own
// work items, so Runs never depend on each other.
type PlanStepDependency struct {
	PlanStepID      string
	DependsOnStepID string
}

// PlanRun is one execution of one exact approved Plan revision. It is stored
// permanently and separately: nothing about a Run is ever reset so the plan can
// be run again, and a later Run leaves every earlier one untouched.
type PlanRun struct {
	ID          string
	ObjectiveID string
	PlanID      string
	// RunKey is opaque and supplied by the harness, unique within the
	// objective. Throughline reads no time, cadence or schedule out of it; it
	// is a correlation handle so a retrying harness cannot create a second Run.
	RunKey string
	// Sequence is the Run's position within its objective, used to give each
	// Run's materialized work items distinct keys. It orders nothing else.
	Sequence int
	Status   PlanRunStatus
	// BindingFingerprint is the digest of the revision and the bindings this
	// Run was created with, so a replayed run_key can be told apart from the
	// same key reused with different content.
	BindingFingerprint string
	StartedBy          string
	StartedAt          time.Time
	ClosedBy           string
	ClosedAt           time.Time
	CloseReason        string
	Version            int
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// RunKeySeparator joins a Plan Step's key to its Run's sequence so each Run's
// materialized work items get their own keys. It is reserved: a key containing
// it can only have been produced by materializing a step, so a hand-written key
// can never occupy the namespace a future run needs.
const RunKeySeparator = "/"

// MaterializedWorkItemKey is the key a Run's copy of a step carries.
func MaterializedWorkItemKey(stepKey string, sequence int) string {
	return fmt.Sprintf("%s%s%d", stepKey, RunKeySeparator, sequence)
}

// MaterializedKeyNamespace is the prefix every key materialized from this step
// starts with, used to check that nothing already occupies it.
func MaterializedKeyNamespace(stepKey string) string {
	return stepKey + RunKeySeparator
}

func NewPlanRun(run PlanRun, now time.Time) (PlanRun, error) {
	run.ID = strings.TrimSpace(run.ID)
	run.ObjectiveID = strings.TrimSpace(run.ObjectiveID)
	run.PlanID = strings.TrimSpace(run.PlanID)
	run.RunKey = strings.TrimSpace(run.RunKey)
	run.StartedBy = strings.TrimSpace(run.StartedBy)
	if run.ID == "" || run.ObjectiveID == "" || run.PlanID == "" || run.RunKey == "" || run.StartedBy == "" {
		return PlanRun{}, errors.New("plan run requires id, objective id, plan id, run key, and starting actor")
	}
	if run.Sequence < 1 {
		return PlanRun{}, errors.New("plan run sequence must be positive")
	}
	if strings.TrimSpace(run.BindingFingerprint) == "" {
		return PlanRun{}, errors.New("plan run requires a binding fingerprint")
	}
	// A Run is created active. There is no preparatory state: every required
	// binding is supplied at creation, so there is nothing a created Run could
	// still be waiting for.
	run.Status = PlanRunActive
	run.Version = 1
	run.StartedAt = now.UTC()
	run.CreatedAt = now.UTC()
	run.UpdatedAt = now.UTC()
	return run, nil
}

// ClosePlanRun is the single explicit ending. A Run never ends on its own from
// the state of its work: an agent or harness asks for a target status, and
// Throughline only checks that the target is allowed.
//
// The success gates themselves are evaluated by the caller and passed in as
// facts, because they aggregate work item state this package cannot read.
func ClosePlanRun(run PlanRun, target PlanRunStatus, actor, reason string, facts RunClosureFacts, now time.Time) (PlanRun, error) {
	actor = strings.TrimSpace(actor)
	reason = strings.TrimSpace(reason)
	if run.Status.Terminal() {
		return PlanRun{}, fmt.Errorf("plan run is already %s and cannot be closed again", run.Status)
	}
	if run.Status != PlanRunActive {
		return PlanRun{}, fmt.Errorf("plan run cannot be closed from %q", run.Status)
	}
	if actor == "" {
		return PlanRun{}, errors.New("closing a plan run requires an actor")
	}
	switch target {
	case PlanRunSucceeded:
		if unmet := facts.unmet(); len(unmet) != 0 {
			return PlanRun{}, fmt.Errorf("plan run cannot succeed: %s", strings.Join(unmet, "; "))
		}
	case PlanRunFailed, PlanRunCancelled:
		if reason == "" {
			return PlanRun{}, fmt.Errorf("closing a plan run as %s requires a rationale", target)
		}
	default:
		return PlanRun{}, fmt.Errorf("plan run cannot be closed as %q", target)
	}
	run.Status = target
	run.ClosedBy = actor
	run.ClosedAt = now.UTC()
	run.CloseReason = reason
	run.Version++
	run.UpdatedAt = now.UTC()
	return run, nil
}

// RunClosureFacts are the aggregated work-item obligations a successful Run
// must satisfy. There is no separate run-wide requirement mechanism: success
// is exactly the sum of what the Run's work items already declare.
type RunClosureFacts struct {
	RequiredStepItemsDone      bool
	RemainingItemsTerminal     bool
	OutputObligationsSatisfied bool
	ActionObligationsSatisfied bool
}

func (f RunClosureFacts) unmet() []string {
	var unmet []string
	for _, check := range []struct {
		satisfied bool
		message   string
	}{
		{f.RequiredStepItemsDone, "every required plan step's work item must be done"},
		{f.RemainingItemsTerminal, "every remaining work item of the run must be terminal"},
		{f.OutputObligationsSatisfied, "required outputs of the run's work items are not satisfied"},
		{f.ActionObligationsSatisfied, "required external actions of the run's work items are not satisfied"},
	} {
		if !check.satisfied {
			unmet = append(unmet, check.message)
		}
	}
	return unmet
}

// RunInputBindingKind is how a binding carries its value. A binding to an
// earlier Run's output is a variant here rather than an entity of its own: it
// is the same relation — this Run's input is that exact value — and it holds
// no invariant the binding does not already have.
type RunInputBindingKind string

const (
	BindingValue          RunInputBindingKind = "value"
	BindingOutputRevision RunInputBindingKind = "output_revision"
	BindingExternal       RunInputBindingKind = "external"
)

func ValidRunInputBindingKind(value RunInputBindingKind) bool {
	return oneOf(value, BindingValue, BindingOutputRevision, BindingExternal)
}

// RunInputBinding is what one Run was actually given for one Plan Input. It is
// written when the Run is created and never changed: a wrong binding is
// answered by cancelling the Run and creating a new one, not by editing what a
// Run claims to have worked from.
type RunInputBinding struct {
	ID          string
	PlanRunID   string
	PlanInputID string
	Name        string
	Kind        RunInputBindingKind
	// Value carries a literal value binding.
	Value string
	// OutputRevisionID names one exact accepted output revision. Throughline
	// never resolves latest, previous or last-successful on the caller's
	// behalf; the harness picks the revision and binds it.
	OutputRevisionID string
	// Locator, SourceVersion and Digest identify external content. Throughline
	// stores and shape-checks them and never fetches the source or computes a
	// digest itself: the caller supplies the identity.
	Locator       string
	SourceVersion string
	Digest        string
	CreatedBy     string
	CreatedAt     time.Time
}

func NewRunInputBinding(binding RunInputBinding, now time.Time) (RunInputBinding, error) {
	binding.ID = strings.TrimSpace(binding.ID)
	binding.PlanRunID = strings.TrimSpace(binding.PlanRunID)
	binding.PlanInputID = strings.TrimSpace(binding.PlanInputID)
	binding.Name = strings.TrimSpace(binding.Name)
	binding.Value = strings.TrimSpace(binding.Value)
	binding.OutputRevisionID = strings.TrimSpace(binding.OutputRevisionID)
	binding.Locator = strings.TrimSpace(binding.Locator)
	binding.SourceVersion = strings.TrimSpace(binding.SourceVersion)
	binding.Digest = strings.TrimSpace(binding.Digest)
	binding.CreatedBy = strings.TrimSpace(binding.CreatedBy)
	if binding.ID == "" || binding.PlanRunID == "" || binding.PlanInputID == "" || binding.Name == "" || binding.CreatedBy == "" {
		return RunInputBinding{}, errors.New("run input binding requires id, run id, plan input id, name, and creating actor")
	}
	switch binding.Kind {
	case BindingValue:
		if binding.Value == "" {
			return RunInputBinding{}, fmt.Errorf("run input binding %q requires a value", binding.Name)
		}
		binding.OutputRevisionID, binding.Locator, binding.SourceVersion, binding.Digest = "", "", "", ""
	case BindingOutputRevision:
		if binding.OutputRevisionID == "" {
			return RunInputBinding{}, fmt.Errorf("run input binding %q requires one exact accepted output revision", binding.Name)
		}
		binding.Value, binding.Locator, binding.SourceVersion, binding.Digest = "", "", "", ""
	case BindingExternal:
		if binding.Locator == "" {
			return RunInputBinding{}, fmt.Errorf("run input binding %q requires a stable locator", binding.Name)
		}
		if binding.SourceVersion == "" && binding.Digest == "" {
			return RunInputBinding{}, fmt.Errorf("run input binding %q requires an immutable source version or a content digest; a bare locator cannot identify what the run used", binding.Name)
		}
		binding.Value, binding.OutputRevisionID = "", ""
	default:
		return RunInputBinding{}, fmt.Errorf("invalid run input binding kind %q", binding.Kind)
	}
	binding.CreatedAt = now.UTC()
	return binding, nil
}

// Fingerprint is the binding's contribution to its Run's creation
// fingerprint: the content that must match for a replayed run_key to mean the
// same Run.
//
// Each field is length-prefixed rather than separator-joined, so no two
// different bindings can serialize the same way whatever their values contain.
func (b RunInputBinding) Fingerprint() string {
	var builder strings.Builder
	for _, field := range []string{b.Name, string(b.Kind), b.Value, b.OutputRevisionID, b.Locator, b.SourceVersion, b.Digest} {
		fmt.Fprintf(&builder, "%d:%s", len(field), field)
	}
	return builder.String()
}

// RunCreationFingerprint identifies what a Run was created from. A run_key
// replayed with the same revision and the same bindings resolves to the
// existing Run; the same key with different content is a conflict rather than
// a silent reinterpretation of a Run that already exists.
func RunCreationFingerprint(planID string, bindings []RunInputBinding) string {
	parts := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		parts = append(parts, binding.Fingerprint())
	}
	sort.Strings(parts)
	digest := sha256.Sum256([]byte(strings.Join(append([]string{planID}, parts...), "\x1e")))
	return hex.EncodeToString(digest[:])
}

// RunGateSatisfied reports whether an item's origin currently permits a new
// claim, an executing transition or the start of an external action, and why
// not when it does not.
//
// The rule is one sentence: work that belongs to a Run executes only while
// that Run is active, work proposed outside a Run never executes, and work
// that predates Runs keeps the behavior it always had.
func RunGateSatisfied(origin WorkItemOrigin, runActive bool) (bool, string) {
	switch origin {
	case OriginPlanStep:
		if runActive {
			return true, ""
		}
		return false, "work belonging to a plan run can only be executed while that run is active"
	case OriginUnplanned:
		return false, "work proposed outside a plan run is not executable; take it into an active run first"
	default:
		return true, ""
	}
}
