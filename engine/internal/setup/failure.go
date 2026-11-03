package setup

import "errors"

// Failure identifies a host configuration stage without exposing addresses or command output.
type Failure int

const (
	HostInspectionFailure Failure = iota + 1
	RouteConflict
	RejectGuardMissing
	ResolverConfigurationFailure
	TunnelConfigurationFailure
)

func (f Failure) Error() string {
	switch f {
	case HostInspectionFailure:
		return "host routes could not be inspected"
	case RouteConflict:
		return "a host route conflicts with Tailmulti"
	case RejectGuardMissing:
		return "a required traffic-blocking route is missing"
	case ResolverConfigurationFailure:
		return "tailnet DNS configuration failed"
	case TunnelConfigurationFailure:
		return "virtual interface configuration failed"
	default:
		return "configuration failed"
	}
}

func FailureReason(err error) string {
	var failure Failure
	if errors.As(err, &failure) {
		return failure.Error()
	}
	return Failure(0).Error()
}
