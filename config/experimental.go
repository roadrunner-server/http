package config

// UnstableFeatures contains features marked experimental
type UnstableFeatures struct {
	HTTPStreamPool bool `mapstructure:"response_streams"`
}
