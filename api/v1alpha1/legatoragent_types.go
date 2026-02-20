/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AutonomyLevel defines the graduated autonomy for an agent.
// +kubebuilder:validation:Enum=observe;recommend;automate-safe;automate-destructive
type AutonomyLevel string

const (
	AutonomyObserve     AutonomyLevel = "observe"
	AutonomyRecommend   AutonomyLevel = "recommend"
	AutonomySafe        AutonomyLevel = "automate-safe"
	AutonomyDestructive AutonomyLevel = "automate-destructive"
)

// ModelTier abstracts model capability rather than naming a specific model.
// +kubebuilder:validation:Enum=fast;standard;reasoning
type ModelTier string

const (
	ModelTierFast      ModelTier = "fast"
	ModelTierStandard  ModelTier = "standard"
	ModelTierReasoning ModelTier = "reasoning"
)

// ReportAction defines what to do on a given event.
// +kubebuilder:validation:Enum=silent;log;notify;escalate
type ReportAction string

const (
	ReportSilent   ReportAction = "silent"
	ReportLog      ReportAction = "log"
	ReportNotify   ReportAction = "notify"
	ReportEscalate ReportAction = "escalate"
)

// EscalationTarget defines where escalations are sent.
// +kubebuilder:validation:Enum=parent;channel;human
type EscalationTarget string

const (
	EscalationParent  EscalationTarget = "parent"
	EscalationChannel EscalationTarget = "channel"
	EscalationHuman   EscalationTarget = "human"
)

// TimeoutAction defines what happens when an escalation times out.
// +kubebuilder:validation:Enum=cancel;proceed;retry
type TimeoutAction string

const (
	TimeoutCancel  TimeoutAction = "cancel"
	TimeoutProceed TimeoutAction = "proceed"
	TimeoutRetry   TimeoutAction = "retry"
)

// TriggerType defines what can trigger an agent run.
// +kubebuilder:validation:Enum=webhook;kubernetes-event
type TriggerType string

const (
	TriggerWebhook         TriggerType = "webhook"
	TriggerKubernetesEvent TriggerType = "kubernetes-event"
)

// SkillSourceType indicates where a skill is loaded from.
// +kubebuilder:validation:Enum=bundled;configmap;git
type SkillSourceType string

const (
	SkillSourceBundled   SkillSourceType = "bundled"
	SkillSourceConfigMap SkillSourceType = "configmap"
	SkillSourceGit       SkillSourceType = "git"
)

// LogLevel defines the logging verbosity.
// +kubebuilder:validation:Enum=debug;info;warn;error
type LogLevel string

// --- Sub-types ---

// ScheduleSpec defines when an agent runs.
type ScheduleSpec struct {
	// cron is a standard cron expression (e.g. "*/5 * * * *").
	// +optional
	Cron string `json:"cron,omitempty"`

	// interval is an alternative to cron (e.g. "300s", "5m").
	// +optional
	Interval string `json:"interval,omitempty"`

	// timezone is an IANA timezone for cron evaluation (default "UTC").
	// +optional
	// +kubebuilder:default="UTC"
	Timezone string `json:"timezone,omitempty"`

	// triggers define event-driven execution.
	// +optional
	Triggers []TriggerSpec `json:"triggers,omitempty"`
}

// TriggerSpec defines an event-based trigger.
type TriggerSpec struct {
	// type is the trigger kind.
	// +required
	Type TriggerType `json:"type"`

	// source identifies the event origin (e.g. "alertmanager").
	// +optional
	Source string `json:"source,omitempty"`

	// filter is a CEL expression evaluated against the event payload.
	// +optional
	Filter string `json:"filter,omitempty"`

	// resources lists Kubernetes resource kinds to watch (for kubernetes-event type).
	// +optional
	Resources []string `json:"resources,omitempty"`

	// reasons lists event reasons to match (for kubernetes-event type).
	// +optional
	Reasons []string `json:"reasons,omitempty"`
}

// ModelSpec configures the LLM for an agent.
type ModelSpec struct {
	// tier selects the model class (fast/standard/reasoning).
	// +required
	// +kubebuilder:default="standard"
	Tier ModelTier `json:"tier"`

	// tokenBudget is the hard max tokens per run.
	// +optional
	// +kubebuilder:default=50000
	TokenBudget int64 `json:"tokenBudget,omitempty"`

	// timeout is the max wall-clock duration per run (e.g. "60s", "5m").
	// +optional
	// +kubebuilder:default="120s"
	Timeout string `json:"timeout,omitempty"`
}

// SkillRef references a skill to load.
type SkillRef struct {
	// name identifies the skill.
	// +required
	Name string `json:"name"`

	// source indicates where the skill lives.
	// +required
	Source string `json:"source"`
}

// CapabilitiesSpec declares required and optional tool capabilities.
type CapabilitiesSpec struct {
	// required capabilities must be satisfiable by the environment.
	// +optional
	Required []string `json:"required,omitempty"`

	// optional capabilities are used if available.
	// +optional
	Optional []string `json:"optional,omitempty"`
}

// GuardrailsSpec defines safety boundaries.
type GuardrailsSpec struct {
	// autonomy is the graduated autonomy level.
	// +required
	// +kubebuilder:default="observe"
	Autonomy AutonomyLevel `json:"autonomy"`

	// allowedActions is a glob list of permitted tool calls (only when autonomy >= automate-safe).
	// +optional
	AllowedActions []string `json:"allowedActions,omitempty"`

	// deniedActions is a glob list of always-blocked tool calls (overrides allowedActions).
	// +optional
	DeniedActions []string `json:"deniedActions,omitempty"`

	// escalation configures how autonomy-ceiling events are handled.
	// +optional
	Escalation *EscalationSpec `json:"escalation,omitempty"`

	// maxIterations is the hard limit on tool-call loops per run.
	// +optional
	// +kubebuilder:default=10
	MaxIterations int32 `json:"maxIterations,omitempty"`

	// maxRetries is the max retries on transient failure.
	// +optional
	// +kubebuilder:default=2
	MaxRetries int32 `json:"maxRetries,omitempty"`

	// approvalMode controls how actions exceeding autonomy are handled.
	// "none" (default): exceed → block.
	// "mutation-gate": exceed → create ApprovalRequest, wait for decision.
	// "plan-first": agent proposes plan, all mutations need approval.
	// "every-action": every non-read action needs approval.
	// +optional
	// +kubebuilder:validation:Enum=none;mutation-gate;plan-first;every-action
	ApprovalMode string `json:"approvalMode,omitempty"`

	// approvalTimeout is the duration to wait for an approval decision before expiring.
	// +optional
	// +kubebuilder:default="30m"
	ApprovalTimeout string `json:"approvalTimeout,omitempty"`
}

// EscalationSpec configures escalation behaviour.
type EscalationSpec struct {
	// target defines where escalations go.
	// +required
	Target EscalationTarget `json:"target"`

	// channelName references a named channel in the LegatorEnvironment (when target=channel).
	// +optional
	ChannelName string `json:"channelName,omitempty"`

	// timeout is how long to wait for a response.
	// +optional
	// +kubebuilder:default="300s"
	Timeout string `json:"timeout,omitempty"`

	// onTimeout defines what happens when the timeout expires.
	// +optional
	// +kubebuilder:default="cancel"
	OnTimeout TimeoutAction `json:"onTimeout,omitempty"`
}

// ObservabilitySpec configures agent telemetry.
type ObservabilitySpec struct {
	// metrics enables Prometheus metric emission.
	// +optional
	// +kubebuilder:default=true
	Metrics bool `json:"metrics,omitempty"`

	// tracing enables OpenTelemetry spans.
	// +optional
	// +kubebuilder:default=true
	Tracing bool `json:"tracing,omitempty"`

	// logLevel sets the logging verbosity.
	// +optional
	// +kubebuilder:default="info"
	LogLevel LogLevel `json:"logLevel,omitempty"`
}

// ReportingSpec configures what happens on different run outcomes.
type ReportingSpec struct {
	// onSuccess defines the action on a successful run.
	// +optional
	// +kubebuilder:default="silent"
	OnSuccess ReportAction `json:"onSuccess,omitempty"`

	// onFailure defines the action on a failed run.
	// +optional
	// +kubebuilder:default="escalate"
	OnFailure ReportAction `json:"onFailure,omitempty"`

	// onFinding defines the action when the agent discovers something noteworthy.
	// +optional
	// +kubebuilder:default="log"
	OnFinding ReportAction `json:"onFinding,omitempty"`
}

// LegatorAgentSpec defines the desired state of an LegatorAgent.
type LegatorAgentSpec struct {
	// description is a human-readable summary of what this agent does.
	// +required
	Description string `json:"description"`

	// emoji is an optional icon for human-friendly display.
	// +optional
	Emoji string `json:"emoji,omitempty"`

	// schedule defines when the agent runs.
	// +required
	Schedule ScheduleSpec `json:"schedule"`

	// model configures the LLM tier and budget.
	// +required
	Model ModelSpec `json:"model"`

	// skills lists the Agent Skills to load.
	// +required
	// +kubebuilder:validation:MinItems=1
	Skills []SkillRef `json:"skills"`

	// capabilities declares required and optional tool capabilities.
	// +optional
	Capabilities *CapabilitiesSpec `json:"capabilities,omitempty"`

	// guardrails defines safety boundaries and escalation policy.
	// +required
	Guardrails GuardrailsSpec `json:"guardrails"`

	// observability configures metrics, tracing, and logging.
	// +optional
	Observability *ObservabilitySpec `json:"observability,omitempty"`

	// reporting configures run outcome actions.
	// +optional
	Reporting *ReportingSpec `json:"reporting,omitempty"`

	// environmentRef names the LegatorEnvironment to bind.
	// +required
	EnvironmentRef string `json:"environmentRef"`

	// paused stops scheduling without deleting the agent.
	// +optional
	Paused bool `json:"paused,omitempty"`
}

// LegatorAgentPhase represents the lifecycle phase of an agent.
// +kubebuilder:validation:Enum=Pending;Ready;Running;Error;Paused
type LegatorAgentPhase string

const (
	LegatorAgentPhasePending LegatorAgentPhase = "Pending"
	LegatorAgentPhaseReady   LegatorAgentPhase = "Ready"
	LegatorAgentPhaseRunning LegatorAgentPhase = "Running"
	LegatorAgentPhaseError   LegatorAgentPhase = "Error"
	LegatorAgentPhasePaused  LegatorAgentPhase = "Paused"
)

// LegatorAgentStatus defines the observed state of LegatorAgent.
type LegatorAgentStatus struct {
	// phase is the current lifecycle phase.
	// +optional
	Phase LegatorAgentPhase `json:"phase,omitempty"`

	// lastRunTime is when the agent last executed.
	// +optional
	LastRunTime *metav1.Time `json:"lastRunTime,omitempty"`

	// nextRunTime is the computed next execution time.
	// +optional
	NextRunTime *metav1.Time `json:"nextRunTime,omitempty"`

	// runCount is the total number of runs.
	// +optional
	RunCount int64 `json:"runCount,omitempty"`

	// consecutiveFailures tracks sequential failures for alerting.
	// +optional
	ConsecutiveFailures int32 `json:"consecutiveFailures,omitempty"`

	// lastRunName is the name of the most recent LegatorRun CR.
	// +optional
	LastRunName string `json:"lastRunName,omitempty"`

	// conditions represent the current state of the LegatorAgent.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Autonomy",type="string",JSONPath=".spec.guardrails.autonomy"
// +kubebuilder:printcolumn:name="Schedule",type="string",JSONPath=".spec.schedule.cron"
// +kubebuilder:printcolumn:name="Last Run",type="date",JSONPath=".status.lastRunTime"
// +kubebuilder:printcolumn:name="Runs",type="integer",JSONPath=".status.runCount"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// LegatorAgent is the Schema for the legators API.
// It defines an autonomous infrastructure agent — its identity, schedule,
// model configuration, skills, capabilities, guardrails, and environment binding.
type LegatorAgent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +required
	Spec   LegatorAgentSpec   `json:"spec"`
	Status LegatorAgentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// LegatorAgentList contains a list of LegatorAgent.
type LegatorAgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LegatorAgent `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LegatorAgent{}, &LegatorAgentList{})
}
