/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Package multicluster provides remote Kubernetes client factories for
// agents that manage clusters other than the one the controller runs on.
//
// When an AgentEnvironment specifies connection.kind: "kubeconfig", the
// factory reads the referenced Secret, builds a rest.Config, and returns
// a client.Client scoped to the remote cluster. For "in-cluster" mode,
// it returns nil (caller uses the default client).
//
// The factory caches clients per (namespace, secret, key, resourceVersion)
// tuple to avoid creating new REST transports on every run.
package multicluster

import (
	"context"
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1alpha1 "github.com/marcus-qen/infraagent/api/v1alpha1"
)

// cacheKey identifies a unique kubeconfig origin.
type cacheKey struct {
	namespace       string
	secretName      string
	secretKey       string
	resourceVersion string
}

// ClientFactory builds Kubernetes clients for agent environments.
type ClientFactory struct {
	// localClient reads Secrets from the management cluster.
	localClient client.Client
	scheme      *runtime.Scheme

	mu    sync.RWMutex
	cache map[cacheKey]client.Client
}

// NewClientFactory creates a multi-cluster client factory.
func NewClientFactory(localClient client.Client, scheme *runtime.Scheme) *ClientFactory {
	return &ClientFactory{
		localClient: localClient,
		scheme:      scheme,
		cache:       make(map[cacheKey]client.Client),
	}
}

// ClientForEnvironment returns a Kubernetes client appropriate for the given
// AgentEnvironment. Returns nil if the environment uses in-cluster connection
// (meaning the caller should use its default client).
func (f *ClientFactory) ClientForEnvironment(ctx context.Context, env *corev1alpha1.AgentEnvironment) (client.Client, error) {
	conn := env.Spec.Connection
	if conn == nil || conn.Kind == "in-cluster" {
		return nil, nil // use default
	}

	if conn.Kind != "kubeconfig" {
		return nil, fmt.Errorf("unsupported connection kind: %q", conn.Kind)
	}

	if conn.Kubeconfig == nil || conn.Kubeconfig.SecretRef == "" {
		return nil, fmt.Errorf("connection kind is 'kubeconfig' but kubeconfig.secretRef is empty")
	}

	key := conn.Kubeconfig.Key
	if key == "" {
		key = "kubeconfig"
	}

	// Read the Secret
	secret := &corev1.Secret{}
	secretKey := types.NamespacedName{
		Namespace: env.Namespace,
		Name:      conn.Kubeconfig.SecretRef,
	}
	if err := f.localClient.Get(ctx, secretKey, secret); err != nil {
		return nil, fmt.Errorf("get kubeconfig secret %s/%s: %w", env.Namespace, conn.Kubeconfig.SecretRef, err)
	}

	// Check cache
	ck := cacheKey{
		namespace:       env.Namespace,
		secretName:      conn.Kubeconfig.SecretRef,
		secretKey:       key,
		resourceVersion: secret.ResourceVersion,
	}

	f.mu.RLock()
	if cached, ok := f.cache[ck]; ok {
		f.mu.RUnlock()
		return cached, nil
	}
	f.mu.RUnlock()

	// Build client from kubeconfig data
	data, ok := secret.Data[key]
	if !ok {
		return nil, fmt.Errorf("secret %s/%s does not contain key %q", env.Namespace, conn.Kubeconfig.SecretRef, key)
	}

	restCfg, err := clientcmd.RESTConfigFromKubeConfig(data)
	if err != nil {
		return nil, fmt.Errorf("parse kubeconfig from secret %s/%s key %q: %w",
			env.Namespace, conn.Kubeconfig.SecretRef, key, err)
	}

	// Apply reasonable defaults
	restCfg.QPS = 20
	restCfg.Burst = 40

	remoteClient, err := client.New(restCfg, client.Options{Scheme: f.scheme})
	if err != nil {
		return nil, fmt.Errorf("create remote client from secret %s/%s: %w",
			env.Namespace, conn.Kubeconfig.SecretRef, err)
	}

	// Cache it
	f.mu.Lock()
	f.cache[ck] = remoteClient
	f.mu.Unlock()

	return remoteClient, nil
}

// InvalidateCache removes all cached clients. Call when secrets change.
func (f *ClientFactory) InvalidateCache() {
	f.mu.Lock()
	f.cache = make(map[cacheKey]client.Client)
	f.mu.Unlock()
}

// InvalidateSecret removes cached clients for a specific secret.
func (f *ClientFactory) InvalidateSecret(namespace, name string) {
	f.mu.Lock()
	for k := range f.cache {
		if k.namespace == namespace && k.secretName == name {
			delete(f.cache, k)
		}
	}
	f.mu.Unlock()
}

// CacheSize returns the number of cached remote clients (for testing/metrics).
func (f *ClientFactory) CacheSize() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.cache)
}
