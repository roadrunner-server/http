package http3

type Config struct {
	// Address is the address to listen on.
	Address string `mapstructure:"address"`
}

/*
http:
  address: :8081
  max_request_size: 1024
  middleware: ["gzip"]
  static:
    dir: "../../../"
    forbid: [""]
    allow: [".txt", ".php"]
  uploads:
    forbid: [".php", ".exe", ".bat"]
  pool:
    num_workers: 1
    max_jobs: 0
    allocate_timeout: 60s
    destroy_timeout: 1s

  http3:
    address: 0.0.0.0:6920
*/
