// Package constants contains application-wide constants.
// For multi-tenant conversion, replace CurrentOrganizationID with dynamic tenant resolution.
package constants

// CurrentOrganizationID is hardcoded to 1 for single-tenant deployment.
// When migrating to multi-tenant SaaS:
// 1. Build tenant middleware to extract organization from subdomain/JWT
// 2. Replace usages of this constant with context-based tenant ID
// 3. No database migrations required - data already scoped by organization_id
const CurrentOrganizationID = "00000000-0000-0000-0000-000000000001"

// DefaultOrganizationSlug is the slug for the default organization
const DefaultOrganizationSlug = "default"
