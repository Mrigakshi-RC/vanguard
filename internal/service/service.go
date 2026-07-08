package service

import (
	"fmt"
	"slices"
)

func ValidateEventStatus(status string) error {
	validStatus := []string{"pending", "processing", "completed"}
	if !slices.Contains(validStatus, status) {
		return fmt.Errorf("invalid status: %s", status)
	}
	return nil
}
