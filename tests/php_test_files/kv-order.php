<?php

require __DIR__ . '/vendor/autoload.php';

use Nyholm\Psr7\Response;
use Nyholm\Psr7\Factory\Psr17Factory;
use Psr\Http\Message\ServerRequestInterface;
use Spiral\RoadRunner\Worker;
use Spiral\RoadRunner\Http\PSR7Worker;
use Spiral\Goridge\RPC\RPC;
use Spiral\RoadRunner\KeyValue\Factory as KVFactory;

// Read some value from the KV storage early on the worker
$rpc = RPC::create('tcp://127.0.0.1:6001');
$kvFactory = new KVFactory($rpc);

$storage = $kvFactory->select('memory-rr');
$storage->get('test_key');

// Just a sample PHP worker hereafter
$worker = Worker::create();
$factory = new Psr17Factory();

$psr7 = new PSR7Worker($worker, $factory, $factory, $factory);

while (true) {
    try {
        $request = $psr7->waitRequest();
        if (!$request instanceof ServerRequestInterface) { // Termination request received
            break;
        }
    } catch (\Throwable $e) {
        $psr7->respond(new Response(400));
        continue;
    }

    try {
        $psr7->respond(new Response(200, [], 'Hello RoadRunner!'));
    } catch (\Throwable $e) {
        $psr7->respond(new Response(500, [], 'Something Went Wrong!'));
        
        $psr7->getWorker()->error((string)$e);
    }
}
