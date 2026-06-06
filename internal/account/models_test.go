package account

import "testing"

func TestRelayTrafficQuotaByMembership(t *testing.T) {
	free := User{Membership: MembershipFree, RelayTrafficUsedBytes: 2, RelayTrafficExtraBytes: 3}
	if got, want := free.RelayTrafficQuotaBytes(), RelayTrafficFreeQuotaBytes+3; got != want {
		t.Fatalf("free quota=%d, want %d", got, want)
	}
	if got, want := free.RelayTrafficRemainingBytes(), RelayTrafficFreeQuotaBytes+1; got != want {
		t.Fatalf("free remaining=%d, want %d", got, want)
	}

	vip := User{Membership: MembershipVIP}
	if got, want := vip.RelayTrafficQuotaBytes(), RelayTrafficVIPQuotaBytes; got != want {
		t.Fatalf("vip quota=%d, want %d", got, want)
	}
}

func TestRelayTrafficRemainingDoesNotGoNegative(t *testing.T) {
	u := User{Membership: MembershipFree, RelayTrafficUsedBytes: RelayTrafficFreeQuotaBytes + 1}
	if got := u.RelayTrafficRemainingBytes(); got != 0 {
		t.Fatalf("remaining=%d, want 0", got)
	}
}

func TestRelayTrafficExtraBalanceAfterBaseQuota(t *testing.T) {
	u := User{
		Membership:             MembershipFree,
		RelayTrafficUsedBytes:  RelayTrafficFreeQuotaBytes + 2,
		RelayTrafficExtraBytes: 3,
	}
	if got, want := u.RelayTrafficRemainingBytes(), int64(3); got != want {
		t.Fatalf("remaining=%d, want %d", got, want)
	}
	if got, want := u.RelayTrafficQuotaBytes(), RelayTrafficFreeQuotaBytes+5; got != want {
		t.Fatalf("quota=%d, want %d", got, want)
	}
}
