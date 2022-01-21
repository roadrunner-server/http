## HTTP Plugin

### Configuration

```yaml
http:
  address: '127.0.0.1:8080'
  access_logs: false
  max_request_size: 256
  middleware: [ "headers", "gzip", "cache", "new_relic", "sendfile" ]
  new_relic:
    app_name: "app"
    license_key: "key"
  ssl:
    address: '0.0.0.0:443'
    acme:
      certs_dir: rr_le_certs
      email: you-email-here@email
      alt_http_port: 80
      alt_tlsalpn_port: 443
      challenge_type: http-01
      use_production_endpoint: true
      domains:
        - your-cool-domains.here
  fcgi:
    address: 'tcp://0.0.0.0:7921'
  http2:
    h2c: false
    max_concurrent_streams: 128
  trusted_subnets:
    - 10.0.0.0/8
    - 127.0.0.0/8
    - 172.16.0.0/12
    - 192.168.0.0/16
    - '::1/128'
    - 'fc00::/7'
    - 'fe80::/10'
  uploads:
    dir: /tmp
    forbid: [ ".php", ".exe", ".bat", ".sh" ]
    allow: [ ".html", ".foo" ]

  # HEADERS MIDDLEWARE
  headers:
    cors:
      allowed_origin: '*'
      allowed_headers: '*'
      allowed_methods: 'GET,POST,PUT,DELETE'
      allow_credentials: true
      exposed_headers: 'Cache-Control,Content-Language,Content-Type,Expires,Last-Modified,Pragma'
      max_age: 600
    request:
      input: custom-header
    response:
      X-Powered-By: RoadRunner

  # Static files middleware
  static:
    dir: .
    forbid: [ ".go" ]
    allow: [ ".txt", ".php" ]
    calculate_etag: false
    weak: false
    request:
      input: custom-header
    response:
      output: output-header
  pool:
    debug: false
    num_workers: 0
    max_jobs: 64
    allocate_timeout: 60s
    destroy_timeout: 60s
    supervisor:
      watch_tick: 1s
      ttl: 0s
      idle_ttl: 10s
      max_worker_memory: 128
      exec_ttl: 60s
```

- YAML configuration is [here](https://github.com/spiral/roadrunner-binary/blob/master/.rr.yaml#L373)

#### Configuration tips:

- If you use ACME provider to obtain certificates, you only need to specify SSL address in the root configuration.
- There is no certificates auto-renewal support yet, but this feature planned for the future. To renew you certificates,
  just re-run RR with `obtain_certificates` set to
  true ([link](https://letsencrypt.org/docs/faq/#what-is-the-lifetime-for-let-s-encrypt-certificates-for-how-long-are-they-valid))
  .

### Worker

```php
<?php

require __DIR__ . '/vendor/autoload.php';

use Nyholm\Psr7\Response;
use Nyholm\Psr7\Factory\Psr17Factory;

use Spiral\RoadRunner\Worker;
use Spiral\RoadRunner\Http\PSR7Worker;


// Create new RoadRunner worker from global environment
$worker = Worker::create();

// Create common PSR-17 HTTP factory
$factory = new Psr17Factory();

//
// Create PSR-7 worker and pass:
//  - RoadRunner worker
//  - PSR-17 ServerRequestFactory
//  - PSR-17 StreamFactory
//  - PSR-17 UploadFilesFactory
//
$psr7 = new PSR7Worker($worker, $factory, $factory, $factory);

while (true) {
    try {
        $request = $psr7->waitRequest();
    } catch (\Throwable $e) {
        // Although the PSR-17 specification clearly states that there can be
	// no exceptions when creating a request, however, some implementations
	// may violate this rule. Therefore, it is recommended to process the 
	// incoming request for errors.
        //
        // Send "Bad Request" response.
        $psr7->respond(new Response(400));
        continue;
    }

    try {
        // Here is where the call to your application code will be located. 
	// For example:
	//
        //  $response = $app->send($request);
        //
        // Reply by the 200 OK response
        $psr7->respond(new Response(200, [], 'Hello RoadRunner!'));
    } catch (\Throwable $e) {
        // In case of any exceptions in the application code, you should handle
	// them and inform the client about the presence of a server error.
	//
        // Reply by the 500 Internal Server Error response
        $psr7->respond(new Response(500, [], 'Something Went Wrong!'));
        
	// Additionally, we can inform the RoadRunner that the processing 
	// of the request failed.
        $worker->error((string)$e);
    }
}

```

## Useful links

- [Error handling](https://github.com/spiral/roadrunner-docs/blob/master/php/error-handling.md)
- [HTTPS](https://github.com/spiral/roadrunner-docs/blob/master/http/https.md)
- [Static content](https://github.com/spiral/roadrunner-docs/blob/master/http/static.md)
- [Golang http middleware](https://github.com/spiral/roadrunner-docs/blob/master/http/middleware.md)


### Available middleware:

<details>
  <summary>Cache middleware</summary>

## Cache (RFC7234) middleware [WIP]

Cache middleware implements http-caching RFC 7234 (not fully yet).  
It handles the following headers:

- `Cache-Control`
- `max-age`

**Responses**:

- `Age`

**HTTP codes**:

- `OK (200)`

**Available backends**:

- `memory`

**Available methods**:

- `GET`

## Configuration

```yaml
http:
  address: 127.0.0.1:44933
  middleware: ["cache"]
  # ...
  cache:
    driver: memory
    cache_methods: ["GET", "HEAD", "POST"] # only GET by default
    config: {}
```

For the worker sample and other docs, please, refer to the [http plugin](http.md)
</details>
<details>
  <summary>GZIP middleware</summary>

- ### GZIP HTTP middleware

```yaml
http:
  address: 127.0.0.1:55555
  max_request_size: 1024
  access_logs: false
  middleware: ["gzip"]

  pool:
    num_workers: 2
    max_jobs: 0
    allocate_timeout: 60s
    destroy_timeout: 60s
```

Used to compress incoming or outgoing data with the default gzip compression level.
</details>
<details>
  <summary>Headers middleware</summary>

# Headers and CORS HTTP middleware

RoadRunner can automatically set up request/response headers and control CORS for your application.

### CORS
To enable CORS headers add the following section to your configuration.

```yaml
http:
  address: 127.0.0.1:44933
  middleware: ["headers"]
  # ...
  headers:
    cors:
      allowed_origin: "*"
      allowed_headers: "*"
      allowed_methods: "GET,POST,PUT,DELETE"
      allow_credentials: true
      exposed_headers: "Cache-Control,Content-Language,Content-Type,Expires,Last-Modified,Pragma"
      max_age: 600
```

> Make sure to declare "headers" middleware.

### Custom headers for Response or Request
You can control additional headers to be set for outgoing responses and headers to be added to the request sent to your application.
```yaml
http:
  # ...
  headers:
      # Automatically add headers to every request passed to PHP.
      request:
        Example-Request-Header: "Value"
    
      # Automatically add headers to every response.
      response:
        X-Powered-By: "RoadRunner"
```
</details>
<details>
  <summary>New Relic middleware</summary>

- ### NewRelic

```yaml
http:
  address: 127.0.0.1:55555
  max_request_size: 1024
  access_logs: false
  middleware: [ "new_relic" ]
  new_relic:
    app_name: "app"
    license_key: "key"

  pool:
    num_workers: 2
    max_jobs: 0
    allocate_timeout: 60s
    destroy_timeout: 60s
```

License key and application name could be set via environment variables: (leave `app_name` and `license_key` empty)

- license_key: `NEW_RELIC_LICENSE_KEY`.
- app_name: `NEW_RELIC_APP_NAME`.

To set the New Relic attributes, the PHP worker should send headers values witing the `rr_newrelic` header key.
Attributes should be separated by the `:`, for example `foo:bar`, where `foo` is a key and `bar` is a value. New Relic
attributes sent from the worker will not appear in the HTTP response, they will be sent directly to the New Relic.

To see the sample of the PHP library, see the @arku31 implementation: https://github.com/arku31/roadrunner-newrelic

The special key which PHP may set to overwrite the transaction name is: `transaction_name`. For
example: `transaction_name:foo` means: set transaction name as `foo`. By default, `RequestURI` is used as the
transaction name.

### Custom PHP Response

```php
        $resp = new \Nyholm\Psr7\Response();
        $rrNewRelic = [
            'shopId:1', //custom data
            'auth:password', //custom data
            'transaction_name:test_transaction' //name - special key to override the name. By default it will use requestUri.
        ];

        $resp = $resp->withHeader('rr_newrelic', $rrNewRelic);
```

Where:
- `shopId:1`, `auth:password` - is a custom data that should be attached to the RR New Relic transaction.
- `transaction_name` - is a special header type to overwrite the default transaction name (`RequestURI`).
</details>
<details>
  <summary>X-Sendfile middleware</summary>

- ### X-Sendfile

```yaml
http:
  address: 127.0.0.1:55555
  max_request_size: 1024
  access_logs: false
  middleware: ["sendfile"]

  pool:
    num_workers: 2
    max_jobs: 0
    allocate_timeout: 60s
    destroy_timeout: 60s
```

HTTP middleware to handle `X-Sendfile` [header](https://github.com/spiral/roadrunner-plugins/issues/9)
Middleware reads the file in 10MB chunks. So, for example for the 5Gb file, only 10MB of RSS will be used. If the file size is smaller than 10MB, the middleware fits the buffer to the file size.

</details>
<details>
  <summary>Static middleware</summary>

# Serving static content

It is possible to serve static content using RoadRunner.

## Enable HTTP Middleware

To enable static content serving use the configuration inside the http section:

```yaml
http:
  # host and port separated by semicolon
  address: 127.0.0.1:44933
  # ...
    static:
      dir: "."
      forbid: [""]
      allow: [".txt", ".php"]
      calculate_etag: false
      weak: false
      request:
        input: "custom-header"
      response:
        output: "output-header"
```

Where:

1. `dir`: path to the directory.
3. `forbid`: file extensions that should not be served.
4. `allow`: file extensions which should be served (empty - serve all except forbidden). If extension presented in both (allow and forbid) hashmaps - that treated as we should forbid file extension.
5. `calculate_etag`: turn on etag computation for the static file.
6. `weak`: use a weak generator (/W), it uses only filename to generate a CRC32 sum. If false - all file content used to generate CRC32 sum.
7. `request/response`: custom headers for the static files.

To combine static content with other middleware, use the following sequence (static will always be the last in the row, file server will apply headers and gzip plugins):

```yaml
http:
  # host and port separated by semicolon
  address: 127.0.0.1:44933
  # ...
  middleware: [ "headers", "gzip" ]
  # ...
  headers:
    # ...
    static:
      dir: "."
      forbid: [""]
      allow: [".txt", ".php"]
      calculate_etag: false
      weak: false
      request:
        input: "custom-header"
      response:
        output: "output-header"
```

</details>
