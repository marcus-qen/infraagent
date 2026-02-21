/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Package inventory manages the infrastructure device inventory.
// ManagedDevice represents any device, server, cluster, database, or service
// that Legator agents can manage.
package inventory

import "time"

// DeviceType categorizes managed devices.
type DeviceType string

const (
	DeviceTypeServer        DeviceType = "server"
	DeviceTypeDatabase      DeviceType = "database"
	DeviceTypeCluster       DeviceType = "cluster"
	DeviceTypeNetworkDevice DeviceType = "network-device"
	DeviceTypeCloudResource DeviceType = "cloud-resource"
	DeviceTypeSaaS          DeviceType = "saas"
	DeviceTypeUnknown       DeviceType = "unknown"
)

// HealthStatus represents the health of a device.
type HealthStatus string

const (
	HealthHealthy     HealthStatus = "healthy"
	HealthWarning     HealthStatus = "warning"
	HealthCritical    HealthStatus = "critical"
	HealthUnknown     HealthStatus = "unknown"
	HealthUnreachable HealthStatus = "unreachable"
)

// ConnectivityMethod describes how the device is connected.
type ConnectivityMethod string

const (
	ConnectHeadscale ConnectivityMethod = "headscale"
	ConnectTailscale ConnectivityMethod = "tailscale"
	ConnectDirect    ConnectivityMethod = "direct"
	ConnectVPN       ConnectivityMethod = "vpn"
)

// ManagedDevice represents a device in the infrastructure inventory.
type ManagedDevice struct {
	// Name is the unique identifier for this device.
	Name string `json:"name"`

	// DisplayName is a human-friendly name.
	DisplayName string `json:"displayName,omitempty"`

	// Type categorizes the device.
	Type DeviceType `json:"type"`

	// Tags for grouping and scoping.
	Tags []string `json:"tags,omitempty"`

	// Addresses contains network addresses.
	Addresses DeviceAddresses `json:"addresses,omitempty"`

	// Hostname is the device's hostname.
	Hostname string `json:"hostname,omitempty"`

	// Connectivity describes how the device is reached.
	Connectivity DeviceConnectivity `json:"connectivity"`

	// Location is the physical or logical location.
	Location DeviceLocation `json:"location,omitempty"`

	// Protocols lists available access protocols.
	Protocols []DeviceProtocol `json:"protocols,omitempty"`

	// ManagedBy lists agents with access to this device.
	ManagedBy []AgentAccess `json:"managedBy,omitempty"`

	// Health is the current health status.
	Health DeviceHealth `json:"health"`
}

// DeviceAddresses contains network addresses.
type DeviceAddresses struct {
	Headscale string `json:"headscale,omitempty"` // 100.x.x.x
	Internal  string `json:"internal,omitempty"`  // Private IP
	External  string `json:"external,omitempty"`  // Public IP (if any)
}

// DeviceConnectivity describes how the device is connected to the mesh.
type DeviceConnectivity struct {
	Method     ConnectivityMethod `json:"method"`
	NodeID     string             `json:"nodeId,omitempty"`     // Headscale node ID
	LastSeen   *time.Time         `json:"lastSeen,omitempty"`
	Online     bool               `json:"online"`
}

// DeviceLocation is the physical or logical location.
type DeviceLocation struct {
	Site string `json:"site,omitempty"`
	Rack string `json:"rack,omitempty"`
	Unit string `json:"unit,omitempty"`
}

// DeviceProtocol describes an access protocol.
type DeviceProtocol struct {
	Type       string `json:"type"`       // ssh, http, https, snmp, sql
	Port       int    `json:"port"`
	Credential string `json:"credential"` // Name in Environment CRD
}

// AgentAccess describes an agent's access to the device.
type AgentAccess struct {
	Agent    string `json:"agent"`
	Autonomy string `json:"autonomy"`
}

// DeviceHealth contains health information.
type DeviceHealth struct {
	Status       HealthStatus `json:"status"`
	LastProbe    *time.Time   `json:"lastProbe,omitempty"`
	LastProbedBy string       `json:"lastProbedBy,omitempty"`
	OpenIssues   int          `json:"openIssues,omitempty"`
	Findings     []Finding    `json:"findings,omitempty"`
}

// Finding is a discovered issue on a device.
type Finding struct {
	ID        string    `json:"id"`
	Severity  string    `json:"severity"` // critical, high, medium, low, info
	Summary   string    `json:"summary"`
	FoundBy   string    `json:"foundBy"`
	FoundAt   time.Time `json:"foundAt"`
}
