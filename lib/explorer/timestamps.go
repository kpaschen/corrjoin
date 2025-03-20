package explorer

import (
	"fmt"
	"time"
)

const (
	FORMAT = "2006-01-02T15:04:05.000Z"
)

func ConvertToUnixTime(input interface{}) (time.Time, error) {
	switch input.(type) {
	case float32:
		timeAsFloat, _ := input.(float32)
		return time.UnixMilli(int64(timeAsFloat) * 1000), nil
	case float64:
		timeAsFloat, _ := input.(float64)
		return time.UnixMilli(int64(timeAsFloat) * 1000), nil
	case int:
		timeAsInt, _ := input.(int)
		return time.Unix(int64(timeAsInt), 0), nil
	default:
		return time.Now(), fmt.Errorf("bad input to ConvertToUnixTime: %v (%T)", input, input)
	}
}
