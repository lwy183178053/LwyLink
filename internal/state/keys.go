package state

import "fmt"

const redisKeyPrefix = "lwylink"

func redisKey(format string, args ...any) string {
	if len(args) == 0 {
		return redisKeyPrefix + ":" + format
	}
	return redisKeyPrefix + ":" + fmt.Sprintf(format, args...)
}
