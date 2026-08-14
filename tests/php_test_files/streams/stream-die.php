<?php

use Spiral\RoadRunner;

ini_set('display_errors', 'stderr');
require dirname(__DIR__) . "/vendor/autoload.php";

$worker = RoadRunner\Worker::create();
$http = new RoadRunner\Http\HttpWorker($worker);

// One chunk, then the process goes away with the stream still open, so RoadRunner
// hits a relay error instead of the end-of-stream frame.
$dieMidStream = static function (): Generator {
    yield "1\n";

    if (function_exists('posix_kill') && function_exists('posix_getpid')) {
        // 9 is SIGKILL spelled out: the constant lives in pcntl, which is not
        // always loaded, and a signal the runtime can handle would let PHP shut
        // down cleanly and close the stream.
        posix_kill(posix_getpid(), 9);
    }

    exit(1);
};

try {
    while ($req = $http->waitRequest()) {
        $http->respond(200, $dieMidStream());
    }
} catch (\Throwable $e) {
    $worker->error($e->getMessage());
}
