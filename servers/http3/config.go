package http3

type Config struct {
	// Address is the address to listen on.
	Address string `mapstructure:"address"`
	// Key defined private server key.
	Key string `mapstructure:"key"`
	// Cert is https certificate.
	Cert string `mapstructure:"cert"`
}
