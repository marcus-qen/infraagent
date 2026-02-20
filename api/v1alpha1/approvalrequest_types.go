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

// ApprovalRequestPhase describes the lifecycle of an approval request.
type ApprovalRequestPhase string

const (
	ApprovalPhasePending  ApprovalRequestPhase = "Pending"
	ApprovalPhaseApproved ApprovalRequestPhase = "Approved"
	ApprovalPhaseDenied   ApprovalRequestPhase = "Denied"
	ApprovalPhaseExpired  ApprovalRequestPhase = "Expired"
)

// ApprovalRequestSpec defines a proposed action awaiting human approval.
type ApprovalRequestSpec struct {
	// agentName is the agent requesting approval.
	// +required
	AgentName string `json:"agentName"`

	// runName is the LegatorRun this request belongs to.
	// +required
	RunName string `json:"runName"`

	// action describes the proposed action.
	// +required
	Action ProposedAction `json:"action"`

	// context provides additional information for the approver.
	// +optional
	Context string `json:"context,omitempty"`

	// timeout is how long to wait for approval before auto-denying.
	// +optional
	// +kubebuilder:default="30m"
	Timeout string `json:"timeout,omitempty"`

	// channels lists where to send the approval request notification.
	// +optional
	Channels []string `json:"channels,omitempty"`
}

// ProposedAction describes what the agent wants to do.
type ProposedAction struct {
	// tool is the tool name (e.g. "kubectl.apply", "ssh.exec").
	// +required
	Tool string `json:"tool"`

	// tier is the action classification.
	// +required
	Tier string `json:"tier"`

	// target describes what the action targets.
	// +required
	Target string `json:"target"`

	// description is a human-readable summary.
	// +required
	Description string `json:"description"`

	// args are the tool arguments (sanitised — no credentials).
	// +optional
	Args map[string]string `json:"args,omitempty"`
}

// ApprovalRequestStatus records the approval decision.
type ApprovalRequestStatus struct {
	// phase is the current state.
	// +kubebuilder:default="Pending"
	Phase ApprovalRequestPhase `json:"phase,omitempty"`

	// decidedBy is who approved or denied (OIDC subject or "system" for timeout).
	// +optional
	DecidedBy string `json:"decidedBy,omitempty"`

	// decidedAt is when the decision was made.
	// +optional
	DecidedAt *metav1.Time `json:"decidedAt,omitempty"`

	// reason is an optional explanation for the decision.
	// +optional
	Reason string `json:"reason,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Agent",type="string",JSONPath=".spec.agentName"
// +kubebuilder:printcolumn:name="Action",type="string",JSONPath=".spec.action.tool"
// +kubebuilder:printcolumn:name="Tier",type="string",JSONPath=".spec.action.tier"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// ApprovalRequest is a proposed agent action awaiting human approval.
type ApprovalRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +required
	Spec ApprovalRequestSpec `json:"spec"`

	// +optional
	Status ApprovalRequestStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ApprovalRequestList contains a list of ApprovalRequests.
type ApprovalRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ApprovalRequest `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ApprovalRequest{}, &ApprovalRequestList{})
}
