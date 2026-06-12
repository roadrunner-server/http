<?php

declare(strict_types=1);

/**
 * Proof-of-concept HTTP worker for the new RoadRunner gRPC interop.
 *
 * The worker acts as a gRPC *client*: it connects to RoadRunner's
 * http.v2.HttpProxyService, pulls one request at a time via FetchRequest,
 * and pushes the response back via SendResponse. The response body carries
 * the current timestamp so we can prove the rr -> http -> worker round-trip.
 *
 * Run (from project root):
 *   ./rr.exe serve -e -c tests/Acceptance/rr.yaml -w tests/Acceptance \
 *       -o "server.command=php worker-poc.php"
 * then hit http://127.0.0.1:8080.
 */

require __DIR__ . '/vendor/autoload.php';

use Google\Protobuf\GPBEmpty;
use RoadRunner\HTTP\DTO\V2\HttpHandlerResponse;
use RoadRunner\HTTP\DTO\V2\HttpHeaderValue;
use RoadRunner\HTTP\DTO\V2\HttpProxyServiceInterface;
use Spiral\Grpc\Client\Config\ConnectionConfig;
use Spiral\Grpc\Client\GrpcClient;
use Spiral\RoadRunner\GRPC\Context;

/** Append a diagnostic line to worker-poc.log next to this file. */
$log =
/*
    tr(...);
/*/
static function (string $message): void {
    \file_put_contents(
        __DIR__ . '/worker-poc.log',
        \sprintf("[%s] %s\n", \date('H:i:s'), $message),
        \FILE_APPEND,
    );
}; // */

// RoadRunner passes its gRPC endpoint via RR_RPC (e.g. tcp://127.0.0.1:6001).
// ext-grpc wants a bare host:port, so strip the tcp:// scheme.
$rpcAddress = (string) (\getenv('RR_RPC') ?: ($_SERVER['RR_RPC'] ?? 'tcp://127.0.0.1:6001'));
$address = \preg_replace('#^tcp://#', '', $rpcAddress) ?? $rpcAddress;

entry:
try {
    $client = GrpcClient::create(new ConnectionConfig($address));
    $context = new Context(['metadata' => [], 'options' => []]);
    $http = $client->service(HttpProxyServiceInterface::class);
    $log(\sprintf('worker started pid=%d address=%s service=%s', \getmypid(), $address, $http::class));

    while (true) {
        // Long-poll: blocks until RoadRunner has a request for this worker.
        $request = $http->FetchRequest($context, new GPBEmpty());

        $log(
            \sprintf(
                'request id=%s %s %s',
                $request->getId(),
                $request->getMethod(),
                $request->getUri(),
            ),
        );

        $body = \sprintf(
            "Hello from the gRPC PoC worker!\nTime: %s\nRequest ID: %s\n",
            \date('c'),
            $request->getId(),
        );

        $response = (new HttpHandlerResponse())
            ->setId($request->getId())
            ->setStatus(200)
            ->setBody($body)
            ->setHeaders([
                'Content-Type' => (new HttpHeaderValue())->setValues(['text/plain; charset=utf-8']),
                'X-Powered-By' => (new HttpHeaderValue())->setValues(['rr-grpc-poc']),
            ]);

        // tr($response);
        $http->SendResponse($context, $response);
        $log('response sent for id=' . $request->getId());
    }
} catch (\Throwable $e) {
    // tr($e);
    $log("error: " . $e->getMessage());
    \usleep(1000_000);
    // goto entry;
}
