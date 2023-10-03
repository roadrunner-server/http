<?php

use Spiral\RoadRunner;
use Spiral\RoadRunner\Payload;

ini_set('display_errors', 'stderr');
require __DIR__ . "/vendor/autoload.php";

$worker = RoadRunner\Worker::create();
$http = new RoadRunner\Http\HttpWorker($worker);

$read = static function (): Generator {
    foreach (\file(__DIR__ . '/test.txt') as $line) {
        try {
            yield $line;
        } catch (RoadRunner\Http\Exception\StreamStoppedException $e) {
            break;
        } catch (\Throwable $e) {
            break;
        }
        usleep(100_000);
    }
};

$respondStream = function (int $status, string $body, array $headers = [], $lastResponse = false): void {
    $head = (string)\json_encode([
        'status'  => $status,
        'headers' => $headers ?: (object)[],
    ], \JSON_THROW_ON_ERROR);

    $this->worker->respond(new Payload($body, $head, $lastResponse));
};

try {
    while ($req = $http->waitRequest()) {
        $respondStream->call($http, 103, '', [
            'Link' => ['</test.txt>; rel=preload; as=document'],
        ]);
                        $respondStream->call($http, 103, '', [
                            'foo' => ['bar'],
                        ]);
        $http->respond(200, $read());
    }
} catch (\Throwable $e) {
    $worker->error($e->getMessage());
}