// SPDX-License-Identifier: GPL-3.0-or-later

// Package ownership implements owner-group scoping for fusion services: it turns
// "who is calling, with which trusted headers" into a Scope and answers who may
// see, create, change or move a resource. It does no authentication, no storage
// access and no Kubernetes calls. See docs/multi-tenancy.md in the repository.
package ownership
