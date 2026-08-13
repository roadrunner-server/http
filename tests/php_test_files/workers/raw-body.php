<?php

/**
 * Echoes the request body back and reports the number of uploads RoadRunner
 * parsed out of it in the X-Uploads header. Under raw_body the body arrives
 * untouched and the count is always zero.
 */

use Spiral\RoadRunner;

ini_set('display_errors', 'stderr');
require dirname(__DIR__) . "/vendor/autoload.php";

$worker = RoadRunner\Worker::create();
$psr7 = new RoadRunner\Http\PSR7Worker(
    $worker,
    new \Nyholm\Psr7\Factory\Psr17Factory(),
    new \Nyholm\Psr7\Factory\Psr17Factory(),
    new \Nyholm\Psr7\Factory\Psr17Factory()
);

while ($req = $psr7->waitRequest()) {
    try {
        $resp = new \Nyholm\Psr7\Response();
        $resp->getBody()->write((string) $req->getBody());

        $psr7->respond($resp->withHeader('X-Uploads', (string) count($req->getUploadedFiles())));
    } catch (\Throwable $e) {
        $psr7->getWorker()->error((string) $e);
    }
}
