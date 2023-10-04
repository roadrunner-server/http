<?php

require __DIR__ . '/vendor/autoload.php';

use Spiral\RoadRunner;

ini_set('display_errors', 'stderr');
require __DIR__ . "/vendor/autoload.php";

$worker = RoadRunner\Worker::create();
$http = new RoadRunner\Http\HttpWorker($worker);
$read = static function (): Generator {
    $i = 0;
    foreach (\file(__DIR__ . '/test.txt') as $line) {
        if (++$i === 3) {
            throw new Exception('test');
        }
        try {
            yield $line;
        } catch (Spiral\RoadRunner\Http\Exception\StreamStoppedException) {
            // Just stop sending data
            return;
        }
    }
};

try {
    while ($req = $http->waitRequest()) {
        $http->respond(200, $read());
    }
} catch (\Throwable $e) {
    $worker->error($e->getMessage());
}