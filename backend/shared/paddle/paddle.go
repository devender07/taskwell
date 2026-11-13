// Package paddle holds what the user and admin services both need to agree on about Paddle subscriptions.
package paddle

// Granting are the subscription statuses that grant access to its plan: past_due is the grace while Paddle retries.
var Granting = []string{"active", "trialing", "past_due"}
