package clientrelease

import (
	"fmt"
	"strconv"
	"strings"
)

func NormalizePlatform(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return DefaultPlatform
	}
	return value
}

func NormalizeVersion(value string) string {
	return strings.TrimSpace(value)
}

func ValidateVersion(version string) error {
	version = NormalizeVersion(version)
	if len(version) < 1 || len(version) > 32 {
		return fmt.Errorf("版本号长度必须是 1-32 位")
	}
	for _, r := range version {
		if !(r >= 'a' && r <= 'z') &&
			!(r >= 'A' && r <= 'Z') &&
			!(r >= '0' && r <= '9') &&
			r != '.' && r != '-' && r != '_' && r != '+' {
			return fmt.Errorf("版本号只能包含字母、数字、点、短横线、下划线或加号")
		}
	}
	return nil
}

func ValidatePlatform(platform string) error {
	platform = NormalizePlatform(platform)
	if len(platform) < 3 || len(platform) > 32 {
		return fmt.Errorf("平台标识长度必须是 3-32 位")
	}
	for _, r := range platform {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '-' && r != '_' {
			return fmt.Errorf("平台标识只能包含小写字母、数字、下划线和短横线")
		}
	}
	return nil
}

func CompareVersions(a, b string) int {
	left := parseVersion(a)
	right := parseVersion(b)
	maxLen := max(len(left.numbers), len(right.numbers))
	for i := 0; i < maxLen; i++ {
		var lv, rv int
		if i < len(left.numbers) {
			lv = left.numbers[i]
		}
		if i < len(right.numbers) {
			rv = right.numbers[i]
		}
		if lv > rv {
			return 1
		}
		if lv < rv {
			return -1
		}
	}
	if left.prerelease == "" && right.prerelease != "" {
		return 1
	}
	if left.prerelease != "" && right.prerelease == "" {
		return -1
	}
	if left.prerelease > right.prerelease {
		return 1
	}
	if left.prerelease < right.prerelease {
		return -1
	}
	return 0
}

func IsNewerVersion(candidate, current string) bool {
	return CompareVersions(candidate, current) > 0
}

type parsedVersion struct {
	numbers    []int
	prerelease string
}

func parseVersion(value string) parsedVersion {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(strings.TrimPrefix(value, "v"), "V")
	mainPart := value
	if idx := strings.IndexAny(mainPart, "+"); idx >= 0 {
		mainPart = mainPart[:idx]
	}
	prerelease := ""
	if idx := strings.Index(mainPart, "-"); idx >= 0 {
		prerelease = strings.ToLower(mainPart[idx+1:])
		mainPart = mainPart[:idx]
	}
	parts := strings.Split(mainPart, ".")
	numbers := make([]int, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			numbers = append(numbers, 0)
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			numbers = append(numbers, 0)
			continue
		}
		numbers = append(numbers, n)
	}
	return parsedVersion{numbers: numbers, prerelease: prerelease}
}
