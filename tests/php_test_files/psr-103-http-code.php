<?php

use Spiral\RoadRunner;

ini_set('display_errors', 'stderr');
require __DIR__ . "/vendor/autoload.php";

$worker = RoadRunner\Worker::create();
$http = new RoadRunner\Http\HttpWorker($worker);
$read = static function (): Generator {
    $limit = 10;
    foreach (\file(__DIR__ . '/test.txt') as $line) {
        foreach (explode('"', $line) as $chunk) {
            try {
                usleep(50_000);
                yield $chunk;
            } catch (Spiral\RoadRunner\Http\Exception\StreamStoppedException $e) {
                // Just stop sending data
                return;
            }
            if (--$limit === 0) {
                return;
            }
        }
    }
};


try {
    while ($req = $http->waitRequest()) {
        $http->respond(100, '', headers: ['X-100' => ['100']], endOfStream: false);
        $http->respond(101, '', headers: ['X-101' => ['101']], endOfStream: false);
        $http->respond(102, '', headers: ['X-102' => ['102']], endOfStream: false);
        $http->respond(103, '', headers: ['Link' => ['</style111.css>; rel=preload; as=style'], 'X-103' => ['103']], endOfStream: false);
        $http->respond(200, $read(), headers: ['X-200' => ['200']], endOfStream: true);
    }
} catch (\Throwable $e) {
    $worker->error($e->getMessage());
}