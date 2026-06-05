package account

import "time"

const (
	MembershipFree = "free"
	MembershipVIP  = "vip"
)

type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	DisplayName  string    `json:"displayName"`
	Role         string    `json:"role"`
	Membership   string    `json:"membership"`
	Disabled     bool      `json:"disabled"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type PublicUser struct {
	ID          int64     `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"displayName"`
	Role        string    `json:"role"`
	Membership  string    `json:"membership"`
	Disabled    bool      `json:"disabled"`
	CreatedAt   time.Time `json:"createdAt"`
}

type UserFilter struct {
	Query  string
	Status string
	Limit  int
	Offset int
}

func (u User) Public() PublicUser {
	return PublicUser{
		ID:          u.ID,
		Username:    u.Username,
		DisplayName: u.DisplayName,
		Role:        u.Role,
		Membership:  normalizeMembership(u.Membership),
		Disabled:    u.Disabled,
		CreatedAt:   u.CreatedAt,
	}
}

func normalizeMembership(value string) string {
	if value == MembershipVIP {
		return MembershipVIP
	}
	return MembershipFree
}
