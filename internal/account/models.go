package account

import "time"

const (
	MembershipFree = "free"
	MembershipVIP  = "vip"

	RelayTrafficFreeQuotaBytes int64 = 5 * 1024 * 1024 * 1024
	RelayTrafficVIPQuotaBytes  int64 = 100 * 1024 * 1024 * 1024
)

type User struct {
	ID                     int64     `json:"id"`
	Username               string    `json:"username"`
	PasswordHash           string    `json:"-"`
	DisplayName            string    `json:"displayName"`
	Role                   string    `json:"role"`
	Membership             string    `json:"membership"`
	Disabled               bool      `json:"disabled"`
	RelayTrafficUsedBytes  int64     `json:"relayTrafficUsedBytes"`
	RelayTrafficExtraBytes int64     `json:"relayTrafficExtraBytes"`
	RelayTrafficPeriod     string    `json:"relayTrafficPeriod"`
	CreatedAt              time.Time `json:"createdAt"`
	UpdatedAt              time.Time `json:"updatedAt"`
}

type PublicUser struct {
	ID                         int64     `json:"id"`
	Username                   string    `json:"username"`
	DisplayName                string    `json:"displayName"`
	Role                       string    `json:"role"`
	Membership                 string    `json:"membership"`
	Disabled                   bool      `json:"disabled"`
	RelayTrafficUsedBytes      int64     `json:"relayTrafficUsedBytes"`
	RelayTrafficExtraBytes     int64     `json:"relayTrafficExtraBytes"`
	RelayTrafficQuotaBytes     int64     `json:"relayTrafficQuotaBytes"`
	RelayTrafficRemainingBytes int64     `json:"relayTrafficRemainingBytes"`
	RelayTrafficPeriod         string    `json:"relayTrafficPeriod"`
	CreatedAt                  time.Time `json:"createdAt"`
}

type UserFilter struct {
	Query  string
	Status string
	Limit  int
	Offset int
}

func (u User) Public() PublicUser {
	return PublicUser{
		ID:                         u.ID,
		Username:                   u.Username,
		DisplayName:                u.DisplayName,
		Role:                       u.Role,
		Membership:                 normalizeMembership(u.Membership),
		Disabled:                   u.Disabled,
		RelayTrafficUsedBytes:      u.RelayTrafficUsedBytes,
		RelayTrafficExtraBytes:     u.RelayTrafficExtraBytes,
		RelayTrafficQuotaBytes:     u.RelayTrafficQuotaBytes(),
		RelayTrafficRemainingBytes: u.RelayTrafficRemainingBytes(),
		RelayTrafficPeriod:         u.RelayTrafficPeriod,
		CreatedAt:                  u.CreatedAt,
	}
}

func (u User) RelayTrafficBaseQuotaBytes() int64 {
	if normalizeMembership(u.Membership) == MembershipVIP {
		return RelayTrafficVIPQuotaBytes
	}
	return RelayTrafficFreeQuotaBytes
}

func (u User) RelayTrafficQuotaBytes() int64 {
	return maxInt64(0, u.RelayTrafficUsedBytes) + u.RelayTrafficRemainingBytes()
}

func (u User) RelayTrafficRemainingBytes() int64 {
	baseRemaining := maxInt64(0, u.RelayTrafficBaseQuotaBytes()-maxInt64(0, u.RelayTrafficUsedBytes))
	return baseRemaining + maxInt64(0, u.RelayTrafficExtraBytes)
}

func normalizeMembership(value string) string {
	if value == MembershipVIP {
		return MembershipVIP
	}
	return MembershipFree
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
