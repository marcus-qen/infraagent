/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

)

// --- dns.query ---

// DNSQueryTool performs DNS lookups (A, AAAA, CNAME, MX, TXT, NS).
// Read-only by design — DNS queries never modify state.
type DNSQueryTool struct {
	resolver *net.Resolver
}

// NewDNSQueryTool creates a DNS query tool with an optional custom nameserver.
func NewDNSQueryTool(nameserver string) *DNSQueryTool {
	r := net.DefaultResolver
	if nameserver != "" {
		r = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{Timeout: 5 * time.Second}
				return d.DialContext(ctx, "udp", nameserver+":53")
			},
		}
	}
	return &DNSQueryTool{resolver: r}
}

func (t *DNSQueryTool) Name() string { return "dns.query" }

func (t *DNSQueryTool) Description() string {
	return "Query DNS records for a domain. Returns records of the specified type (A, AAAA, CNAME, MX, TXT, NS). Read-only."
}

func (t *DNSQueryTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"domain": map[string]interface{}{
				"type":        "string",
				"description": "Domain name to query",
			},
			"type": map[string]interface{}{
				"type":        "string",
				"description": "Record type: A, AAAA, CNAME, MX, TXT, NS (default: A)",
			},
		},
		"required": []string{"domain"},
	}
}

func (t *DNSQueryTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	domain, _ := args["domain"].(string)
	if domain == "" {
		return "", fmt.Errorf("domain is required")
	}

	recordType, _ := args["type"].(string)
	if recordType == "" {
		recordType = "A"
	}
	recordType = strings.ToUpper(recordType)

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	result := dnsResult{
		Domain: domain,
		Type:   recordType,
	}

	var err error
	switch recordType {
	case "A":
		ips, e := t.resolver.LookupIP(ctx, "ip4", domain)
		err = e
		for _, ip := range ips {
			result.Records = append(result.Records, ip.String())
		}
	case "AAAA":
		ips, e := t.resolver.LookupIP(ctx, "ip6", domain)
		err = e
		for _, ip := range ips {
			result.Records = append(result.Records, ip.String())
		}
	case "CNAME":
		cname, e := t.resolver.LookupCNAME(ctx, domain)
		err = e
		if cname != "" {
			result.Records = append(result.Records, cname)
		}
	case "MX":
		mxs, e := t.resolver.LookupMX(ctx, domain)
		err = e
		for _, mx := range mxs {
			result.Records = append(result.Records, fmt.Sprintf("%d %s", mx.Pref, mx.Host))
		}
	case "TXT":
		txts, e := t.resolver.LookupTXT(ctx, domain)
		err = e
		result.Records = txts
	case "NS":
		nss, e := t.resolver.LookupNS(ctx, domain)
		err = e
		for _, ns := range nss {
			result.Records = append(result.Records, ns.Host)
		}
	default:
		return "", fmt.Errorf("unsupported record type: %s (use A, AAAA, CNAME, MX, TXT, NS)", recordType)
	}

	if err != nil {
		result.Error = err.Error()
	}

	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}

// Capability implements ClassifiableTool.
func (t *DNSQueryTool) Capability() ToolCapability {
	return ToolCapability{
		Domain:         "dns",
		SupportedTiers: []ActionTier{TierRead},
	}
}

// ClassifyAction implements ClassifiableTool — always read.
func (t *DNSQueryTool) ClassifyAction(args map[string]interface{}) ActionClassification {
	return ActionClassification{
		Tier:        TierRead,
		Description: "DNS lookup (read-only)",
	}
}

// --- dns.reverse ---

// DNSReverseTool performs reverse DNS lookups (IP → hostname).
type DNSReverseTool struct {
	resolver *net.Resolver
}

// NewDNSReverseTool creates a reverse DNS tool with an optional custom nameserver.
func NewDNSReverseTool(nameserver string) *DNSReverseTool {
	r := net.DefaultResolver
	if nameserver != "" {
		r = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{Timeout: 5 * time.Second}
				return d.DialContext(ctx, "udp", nameserver+":53")
			},
		}
	}
	return &DNSReverseTool{resolver: r}
}

func (t *DNSReverseTool) Name() string { return "dns.reverse" }

func (t *DNSReverseTool) Description() string {
	return "Reverse DNS lookup: IP address to hostname(s). Read-only."
}

func (t *DNSReverseTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"ip": map[string]interface{}{
				"type":        "string",
				"description": "IP address to reverse-lookup",
			},
		},
		"required": []string{"ip"},
	}
}

func (t *DNSReverseTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	ip, _ := args["ip"].(string)
	if ip == "" {
		return "", fmt.Errorf("ip is required")
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	names, err := t.resolver.LookupAddr(ctx, ip)

	result := dnsResult{
		Domain:  ip,
		Type:    "PTR",
		Records: names,
	}
	if err != nil {
		result.Error = err.Error()
	}

	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}

// Capability implements ClassifiableTool.
func (t *DNSReverseTool) Capability() ToolCapability {
	return ToolCapability{
		Domain:         "dns",
		SupportedTiers: []ActionTier{TierRead},
	}
}

// ClassifyAction implements ClassifiableTool — always read.
func (t *DNSReverseTool) ClassifyAction(args map[string]interface{}) ActionClassification {
	return ActionClassification{
		Tier:        TierRead,
		Description: "reverse DNS lookup (read-only)",
	}
}

// dnsResult holds the output of a DNS query.
type dnsResult struct {
	Domain  string   `json:"domain"`
	Type    string   `json:"type"`
	Records []string `json:"records,omitempty"`
	Error   string   `json:"error,omitempty"`
}
