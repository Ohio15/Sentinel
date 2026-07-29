package api

import "testing"

// TestDecideCertBinding covers the full warn-vs-enforce truth table for the
// token→cert binding decision. This is the branch that governs whether a
// cert-holding agent authenticating by token over the tunnel is allowed (WARN)
// or rejected (enforce). The cert-lookup itself needs a live *pgxpool.Pool, so
// the decision is factored out into this pure function to keep it testable.
func TestDecideCertBinding(t *testing.T) {
	tests := []struct {
		name       string
		holdsCert  bool
		enforce    bool
		wantHolds  bool
		wantEnf    bool
		wantReject bool
	}{
		{
			name:       "no cert, warn mode -> allow, no signal",
			holdsCert:  false,
			enforce:    false,
			wantHolds:  false,
			wantEnf:    false,
			wantReject: false,
		},
		{
			name:       "no cert, enforce mode -> allow (nothing to bind)",
			holdsCert:  false,
			enforce:    true,
			wantHolds:  false,
			wantEnf:    true,
			wantReject: false,
		},
		{
			name:       "holds cert, warn mode -> allow but flag (OBSERVE)",
			holdsCert:  true,
			enforce:    false,
			wantHolds:  true,
			wantEnf:    false,
			wantReject: false,
		},
		{
			name:       "holds cert, enforce mode -> reject",
			holdsCert:  true,
			enforce:    true,
			wantHolds:  true,
			wantEnf:    true,
			wantReject: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decideCertBinding(tt.holdsCert, tt.enforce)
			if got.HoldsCert != tt.wantHolds {
				t.Errorf("HoldsCert = %v, want %v", got.HoldsCert, tt.wantHolds)
			}
			if got.Enforced != tt.wantEnf {
				t.Errorf("Enforced = %v, want %v", got.Enforced, tt.wantEnf)
			}
			if got.Reject != tt.wantReject {
				t.Errorf("Reject = %v, want %v", got.Reject, tt.wantReject)
			}
		})
	}
}

// TestDecideCertBinding_RejectRequiresBoth asserts the safety invariant that a
// connection is only ever rejected when the agent BOTH holds a cert AND
// enforcement is on — i.e. WARN mode is guaranteed to be zero functional change.
func TestDecideCertBinding_RejectRequiresBoth(t *testing.T) {
	for _, holds := range []bool{false, true} {
		for _, enforce := range []bool{false, true} {
			d := decideCertBinding(holds, enforce)
			wantReject := holds && enforce
			if d.Reject != wantReject {
				t.Errorf("decideCertBinding(holds=%v, enforce=%v).Reject = %v, want %v",
					holds, enforce, d.Reject, wantReject)
			}
			if !enforce && d.Reject {
				t.Errorf("WARN mode (enforce=false) must never reject; holds=%v", holds)
			}
		}
	}
}
