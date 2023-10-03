<?php

require __DIR__ . '/vendor/autoload.php';

use RoadRunner\Centrifugo\CentrifugoWorker;
use RoadRunner\Centrifugo\Payload;
use RoadRunner\Centrifugo\Request;
use Spiral\RoadRunner\Worker;
use RoadRunner\Centrifugo\Request\RequestFactory;

$worker = Worker::create();
$requestFactory = new RequestFactory($worker);

// Create a new Centrifugo Worker from global environment
$centrifugoWorker = new CentrifugoWorker($worker, $requestFactory);

while ($request = $centrifugoWorker->waitRequest()) {

    if ($request instanceof Request\Connect) {
        try {
            // Do something
            $request->respond(new Payload\ConnectResponse(
                // ...
            ));

            // You can also disconnect connection
            $request->disconnect('500', 'Connection is not allowed.');
        } catch (\Throwable $e) {
            $request->error($e->getCode(), $e->getMessage());
        }

        continue;
    }

    if ($request instanceof Request\Refresh) {
        try {
            // Do something
            $request->respond(new Payload\RefreshResponse(
                // ...
            ));
        } catch (\Throwable $e) {
            $request->error($e->getCode(), $e->getMessage());
        }

        continue;
    }

    if ($request instanceof Request\Subscribe) {
        try {
            // Do something
            $request->respond(new Payload\SubscribeResponse(
                // ...
            ));

            // You can also disconnect connection
            $request->disconnect('500', 'Connection is not allowed.');
        } catch (\Throwable $e) {
            $request->error($e->getCode(), $e->getMessage());
        }

        continue;
    }

    if ($request instanceof Request\Publish) {
        try {
            // Do something
            $request->respond(new Payload\PublishResponse(
                // ...
            ));

            // You can also disconnect connection
            $request->disconnect('500', 'Connection is not allowed.');
        } catch (\Throwable $e) {
            $request->error($e->getCode(), $e->getMessage());
        }

        continue;
    }

    if ($request instanceof Request\RPC) {
        try {
            $response = $router->handle(
                new Request(uri: $request->method, data: $request->data)
            ); // ['user' => ['id' => 1, 'username' => 'john_smith']]

            $request->respond(new Payload\RPCResponse(
                data: $response
            ));
        } catch (\Throwable $e) {
            $request->error($e->getCode(), $e->getMessage());
        }

        continue;
    }
}