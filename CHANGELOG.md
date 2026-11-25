# Changelog

## Unreleased

### Bug Fixes

* Apply the configured ACME issuer settings and use the same configuration for certificate renewal.
* Remove multipart temp files when the request fails to parse.

## [6.1.1](https://github.com/roadrunner-server/http/compare/v6.1.0...v6.1.1) (2026-11-25)


### Bug Fixes

* **acme:** apply issuer settings and restore certificate renewal ([779fcd5](https://github.com/roadrunner-server/http/commit/779fcd54b792d7ec76cb4e94f48a8cba19ce38e5))
* **acme:** apply issuer settings and restore certificate renewal ([#307](https://github.com/roadrunner-server/http/issues/307)) ([ca3fbb3](https://github.com/roadrunner-server/http/commit/ca3fbb3e17ab15c061ef663d73af1d82259cce2a))
* **handler:** remove multipart temp files when the request fails to parse ([aff007f](https://github.com/roadrunner-server/http/commit/aff007f3475a0503531f1c7567fc285a7259285b))
* **handler:** remove multipart temp files when the request fails to parse ([a4bd7bf](https://github.com/roadrunner-server/http/commit/a4bd7bf92e176a5e225f7d6c9d6d72ba5daf24c5))

## [6.1.0](https://github.com/roadrunner-server/http/compare/v6.0.0...v6.1.0) (2026-10-16)


### Features

* start the HTTP/3 server without experimental mode ([#294](https://github.com/roadrunner-server/http/issues/294)) ([7c30310](https://github.com/roadrunner-server/http/commit/7c30310b80bc84256ad6bd1234eca45e81d136bd))
