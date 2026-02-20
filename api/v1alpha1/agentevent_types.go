/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AgentEventSeverity classifies event importance.
type AgentEventSeverity string

const (
	EventSeverityCritical AgentEventSeverity = "critical"
	EventSeverityWarning  AgentEventSeverity = "warning"
	EventSeverityInfo     AgentEventSeverity = "info"
)

// AgentEventPhase tracks event processing.
type AgentEventPhase string

const (
	EventPhaseNew       AgentEventPhase = "New"
	EventPhaseDelivered AgentEventPhase = "Delivered"
	EventPhaseConsumed  AgentEventPhase = "Consumed"
	EventPhaseExpired   AgentEventPhase = "Expired"
)

// AgentEventSpec defines a finding or signal published by an agent.
type AgentEventSpec struct {
	// sourceAgent is the agent that published this event.
	// +required
	SourceAgent string `json:"sourceAgent"`

	// sourceRun is the LegatorRun that produced this event.
	// +optional
	SourceRun string `json:"sourceRun,omitempty"`

	// eventType classifies the event (e.g. "finding", "alert", "recommendation").
	// +required
	EventType string `json:"eventType"`

	// severity indicates the importance.
	// +required
	// +kubebuilder:validation:Enum="critical";"warning";"info"
	Severity AgentEventSeverity `json:"severity"`

	// summary is a one-line human-readable description.
	// +required
	Summary string `json:"summary"`

	// detail provides full context for consuming agents.
	// +optional
	Detail string `json:"detail,omitempty"`

	// targetAgent is a specific agent to trigger (optional — for directed events).
	// +optional
	TargetAgent string `json:"targetAgent,omitempty"`

	// labels are key-value pairs for filtering and routing.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// ttl is how long the event remains actionable.
	// +optional
	// +kubebuilder:default="1h"
	TTL string `json:"ttl,omitempty"`
}

// AgentEventStatus tracks consumption of the event.
type AgentEventStatus struct {
	// phase is the current processing state.
	// +kubebuilder:default="New"
	Phase AgentEventPhase `json:"phase,omitempty"`

	// consumedBy lists agents that have processed this event.
	// +optional
	ConsumedBy []EventConsumer `json:"consumedBy,omitempty"`

	// triggeredRuns lists LegatorRuns triggered by this event.
	// +optional
	TriggeredRuns []string `json:"triggeredRuns,omitempty"`
}

// EventConsumer records which agent consumed an event and when.
type EventConsumer struct {
	// agent is the consuming agent's name.
	Agent string `json:"agent"`

	// consumedAt is when the agent processed this event.
	ConsumedAt metav1.Time `json:"consumedAt"`

	// runName is the LegatorRun triggered by consumption.
	// +optional
	RunName string `json:"runName,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Source",type="string",JSONPath=".spec.sourceAgent"
// +kubebuilder:printcolumn:name="Type",type="string",JSONPath=".spec.eventType"
// +kubebuilder:printcolumn:name="Severity",type="string",JSONPath=".spec.severity"
// +kubebuilder:printcolumn:name="Summary",type="string",JSONPath=".spec.summary",priority=1
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// AgentEvent is a finding, alert, or signal published by an agent for
// consumption by other agents or the dashboard.
type AgentEvent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +required
	Spec AgentEventSpec `json:"spec"`

	// +optional
	Status AgentEventStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AgentEventList contains a list of AgentEvents.
type AgentEventList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AgentEvent `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AgentEvent{}, &AgentEventList{})
}
