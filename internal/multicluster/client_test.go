/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package multicluster

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	corev1alpha1 "github.com/marcus-qen/infraagent/api/v1alpha1"
)

func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = corev1alpha1.AddToScheme(s)
	return s
}

func TestClientForEnvironment_InCluster(t *testing.T) {
	s := newScheme()
	fc := fake.NewClientBuilder().WithScheme(s).Build()
	factory := NewClientFactory(fc, s)

	// nil connection
	env := &corev1alpha1.AgentEnvironment{
		ObjectMeta: metav1.ObjectMeta{Name: "env", Namespace: "default"},
		Spec:       corev1alpha1.AgentEnvironmentSpec{},
	}
	c, err := factory.ClientForEnvironment(context.Background(), env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c != nil {
		t.Fatal("expected nil client for in-cluster mode")
	}

	// explicit in-cluster
	env.Spec.Connection = &corev1alpha1.ConnectionSpec{Kind: "in-cluster"}
	c, err = factory.ClientForEnvironment(context.Background(), env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c != nil {
		t.Fatal("expected nil client for in-cluster mode")
	}
}

func TestClientForEnvironment_UnsupportedKind(t *testing.T) {
	s := newScheme()
	fc := fake.NewClientBuilder().WithScheme(s).Build()
	factory := NewClientFactory(fc, s)

	env := &corev1alpha1.AgentEnvironment{
		ObjectMeta: metav1.ObjectMeta{Name: "env", Namespace: "default"},
		Spec: corev1alpha1.AgentEnvironmentSpec{
			Connection: &corev1alpha1.ConnectionSpec{Kind: "magic"},
		},
	}
	_, err := factory.ClientForEnvironment(context.Background(), env)
	if err == nil {
		t.Fatal("expected error for unsupported kind")
	}
}

func TestClientForEnvironment_MissingSecretRef(t *testing.T) {
	s := newScheme()
	fc := fake.NewClientBuilder().WithScheme(s).Build()
	factory := NewClientFactory(fc, s)

	env := &corev1alpha1.AgentEnvironment{
		ObjectMeta: metav1.ObjectMeta{Name: "env", Namespace: "default"},
		Spec: corev1alpha1.AgentEnvironmentSpec{
			Connection: &corev1alpha1.ConnectionSpec{
				Kind:       "kubeconfig",
				Kubeconfig: &corev1alpha1.KubeconfigRef{SecretRef: ""},
			},
		},
	}
	_, err := factory.ClientForEnvironment(context.Background(), env)
	if err == nil {
		t.Fatal("expected error for empty secretRef")
	}
}

func TestClientForEnvironment_SecretNotFound(t *testing.T) {
	s := newScheme()
	fc := fake.NewClientBuilder().WithScheme(s).Build()
	factory := NewClientFactory(fc, s)

	env := &corev1alpha1.AgentEnvironment{
		ObjectMeta: metav1.ObjectMeta{Name: "env", Namespace: "default"},
		Spec: corev1alpha1.AgentEnvironmentSpec{
			Connection: &corev1alpha1.ConnectionSpec{
				Kind:       "kubeconfig",
				Kubeconfig: &corev1alpha1.KubeconfigRef{SecretRef: "nonexistent"},
			},
		},
	}
	_, err := factory.ClientForEnvironment(context.Background(), env)
	if err == nil {
		t.Fatal("expected error for missing secret")
	}
}

func TestClientForEnvironment_SecretMissingKey(t *testing.T) {
	s := newScheme()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "my-kc", Namespace: "default", ResourceVersion: "1"},
		Data:       map[string][]byte{"wrong-key": []byte("data")},
	}
	fc := fake.NewClientBuilder().WithScheme(s).WithObjects(secret).Build()
	factory := NewClientFactory(fc, s)

	env := &corev1alpha1.AgentEnvironment{
		ObjectMeta: metav1.ObjectMeta{Name: "env", Namespace: "default"},
		Spec: corev1alpha1.AgentEnvironmentSpec{
			Connection: &corev1alpha1.ConnectionSpec{
				Kind:       "kubeconfig",
				Kubeconfig: &corev1alpha1.KubeconfigRef{SecretRef: "my-kc"},
			},
		},
	}
	_, err := factory.ClientForEnvironment(context.Background(), env)
	if err == nil {
		t.Fatal("expected error for missing key in secret")
	}
}

func TestClientForEnvironment_InvalidKubeconfig(t *testing.T) {
	s := newScheme()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "my-kc", Namespace: "default", ResourceVersion: "1"},
		Data:       map[string][]byte{"kubeconfig": []byte("this is not a valid kubeconfig")},
	}
	fc := fake.NewClientBuilder().WithScheme(s).WithObjects(secret).Build()
	factory := NewClientFactory(fc, s)

	env := &corev1alpha1.AgentEnvironment{
		ObjectMeta: metav1.ObjectMeta{Name: "env", Namespace: "default"},
		Spec: corev1alpha1.AgentEnvironmentSpec{
			Connection: &corev1alpha1.ConnectionSpec{
				Kind:       "kubeconfig",
				Kubeconfig: &corev1alpha1.KubeconfigRef{SecretRef: "my-kc"},
			},
		},
	}
	_, err := factory.ClientForEnvironment(context.Background(), env)
	if err == nil {
		t.Fatal("expected error for invalid kubeconfig")
	}
}

func TestClientForEnvironment_ValidKubeconfig(t *testing.T) {
	// A minimal valid kubeconfig pointing to a non-existent server.
	// We can build the client, we just can't use it to talk to anything.
	kubeconfig := `
apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://127.0.0.1:6443
    insecure-skip-tls-verify: true
  name: test
contexts:
- context:
    cluster: test
    user: test
  name: test
current-context: test
users:
- name: test
  user:
    token: fake-token
`
	s := newScheme()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "remote-kc", Namespace: "infra", ResourceVersion: "42"},
		Data:       map[string][]byte{"kubeconfig": []byte(kubeconfig)},
	}
	fc := fake.NewClientBuilder().WithScheme(s).WithObjects(secret).Build()
	factory := NewClientFactory(fc, s)

	env := &corev1alpha1.AgentEnvironment{
		ObjectMeta: metav1.ObjectMeta{Name: "env", Namespace: "infra"},
		Spec: corev1alpha1.AgentEnvironmentSpec{
			Connection: &corev1alpha1.ConnectionSpec{
				Kind:       "kubeconfig",
				Kubeconfig: &corev1alpha1.KubeconfigRef{SecretRef: "remote-kc"},
			},
		},
	}

	c, err := factory.ClientForEnvironment(context.Background(), env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil client for kubeconfig mode")
	}

	// Cache should have 1 entry
	if factory.CacheSize() != 1 {
		t.Fatalf("expected cache size 1, got %d", factory.CacheSize())
	}

	// Second call should hit cache
	c2, err := factory.ClientForEnvironment(context.Background(), env)
	if err != nil {
		t.Fatalf("unexpected error on cached call: %v", err)
	}
	if c2 == nil {
		t.Fatal("expected non-nil client from cache")
	}
	if factory.CacheSize() != 1 {
		t.Fatalf("cache should still be 1, got %d", factory.CacheSize())
	}
}

func TestClientForEnvironment_CustomKey(t *testing.T) {
	kubeconfig := `
apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://10.0.0.1:6443
    insecure-skip-tls-verify: true
  name: prod
contexts:
- context:
    cluster: prod
    user: prod
  name: prod
current-context: prod
users:
- name: prod
  user:
    token: prod-token
`
	s := newScheme()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "kc-secret", Namespace: "default", ResourceVersion: "5"},
		Data:       map[string][]byte{"my-config": []byte(kubeconfig)},
	}
	fc := fake.NewClientBuilder().WithScheme(s).WithObjects(secret).Build()
	factory := NewClientFactory(fc, s)

	env := &corev1alpha1.AgentEnvironment{
		ObjectMeta: metav1.ObjectMeta{Name: "env", Namespace: "default"},
		Spec: corev1alpha1.AgentEnvironmentSpec{
			Connection: &corev1alpha1.ConnectionSpec{
				Kind: "kubeconfig",
				Kubeconfig: &corev1alpha1.KubeconfigRef{
					SecretRef: "kc-secret",
					Key:       "my-config",
				},
			},
		},
	}
	c, err := factory.ClientForEnvironment(context.Background(), env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestInvalidateCache(t *testing.T) {
	s := newScheme()
	fc := fake.NewClientBuilder().WithScheme(s).Build()
	factory := NewClientFactory(fc, s)

	// Manually inject a cache entry
	factory.mu.Lock()
	factory.cache[cacheKey{namespace: "a", secretName: "b"}] = nil
	factory.cache[cacheKey{namespace: "c", secretName: "d"}] = nil
	factory.mu.Unlock()

	if factory.CacheSize() != 2 {
		t.Fatalf("expected 2, got %d", factory.CacheSize())
	}

	factory.InvalidateCache()
	if factory.CacheSize() != 0 {
		t.Fatalf("expected 0 after invalidate, got %d", factory.CacheSize())
	}
}

func TestInvalidateSecret(t *testing.T) {
	s := newScheme()
	fc := fake.NewClientBuilder().WithScheme(s).Build()
	factory := NewClientFactory(fc, s)

	factory.mu.Lock()
	factory.cache[cacheKey{namespace: "ns", secretName: "s1", resourceVersion: "1"}] = nil
	factory.cache[cacheKey{namespace: "ns", secretName: "s1", resourceVersion: "2"}] = nil
	factory.cache[cacheKey{namespace: "ns", secretName: "s2", resourceVersion: "1"}] = nil
	factory.mu.Unlock()

	factory.InvalidateSecret("ns", "s1")

	if factory.CacheSize() != 1 {
		t.Fatalf("expected 1 after invalidating s1, got %d", factory.CacheSize())
	}
}
