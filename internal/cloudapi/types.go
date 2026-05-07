package cloudapi

import "time"

const DefaultBaseURL = "https://runtree.dev"

type DeviceLoginStartResponse struct {
	DeviceCode          string    `json:"device_code"`
	VerificationURL     string    `json:"verification_url"`
	ExpiresAt           time.Time `json:"expires_at"`
	PollIntervalSeconds int       `json:"poll_interval_seconds"`
}

type DeviceLoginPollResponse struct {
	Status        string `json:"status"`
	AccessToken   string `json:"access_token,omitempty"`
	AccountHandle string `json:"account_handle,omitempty"`
	BaseURL       string `json:"base_url,omitempty"`
}

type MeResponse struct {
	AccountHandle           string `json:"account_handle"`
	Plan                    string `json:"plan"`
	StableAliasLimit        int    `json:"stable_alias_limit"`
	ConcurrentExposureLimit int    `json:"concurrent_exposure_limit"`
}

type CreateExposureRequest struct {
	ProjectName  string `json:"project_name"`
	InstanceName string `json:"instance_name"`
	Branch       string `json:"branch,omitempty"`
	LocalURL     string `json:"local_url"`
	LocalPort    int    `json:"local_port"`
}

type TunnelLaunchConfig struct {
	Provider        string            `json:"provider"`
	Kind            string            `json:"kind"`
	ConfigTemplate  string            `json:"config_template"`
	CredentialsJSON string            `json:"credentials_json"`
	Args            []string          `json:"args,omitempty"`
	Env             map[string]string `json:"env,omitempty"`
}

type CreateExposureResponse struct {
	ExposureID               string             `json:"exposure_id"`
	PublicURL                string             `json:"public_url"`
	HeartbeatIntervalSeconds int                `json:"heartbeat_interval_seconds"`
	UpgradeURL               string             `json:"upgrade_url,omitempty"`
	Launch                   TunnelLaunchConfig `json:"launch"`
}

type apiErrorEnvelope struct {
	Error APIError `json:"error"`
}

type APIError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	UpgradeURL string `json:"upgrade_url,omitempty"`
	StatusCode int    `json:"-"`
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return "cloud API request failed"
}
