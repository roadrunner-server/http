<?php

require __DIR__ . '/vendor/autoload.php';

use RoadRunner\Centrifugo\CentrifugoWorker;
use RoadRunner\Centrifugo\Payload;

use RoadRunner\Centrifugo\Request;
use RoadRunner\Centrifugo\Request\RequestFactory;
use Spiral\RoadRunner\Worker;

$worker = Worker::create();
$requestFactory = new RequestFactory($worker);

// Create a new Centrifugo Worker from global environment

$centrifugoWorker = new CentrifugoWorker($worker, $requestFactory);

while ($request = $centrifugoWorker->waitRequest()) {
    if ($request instanceof Request\Invalid) {
        $errorMessage = $request->getException()->getMessage();

        if ($request->getException() instanceof \RoadRunner\Centrifugo\Exception\InvalidRequestTypeException) {
            $payload = $request->getException()->payload;
        }

        continue;
    }


    if ($request instanceof Request\Connect) {
        try {
            // Do something
            $request->respond(new Payload\ConnectResponse(
                user: 'ca94a2db-b5e8-4551-986b-5bad3e871fd1',
                data: ['user' => 'ca94a2db-b5e8-4551-986b-5bad3e871fd1'],
                channels: [
                            '1',
                            '2',
                            '3',
                            '4',
                            '5',
                            '6',
                            '7',
                            '8',
                            '9',
                            '10',
                            '11',
                            '111',
                            '112',
                            '113',
                            '114',
                            '115',
                            '611',
                            '117',
                            '118',
                            '119',
                            '1110',
                            '1111'
                ],

            ));
        } catch (\Throwable $e) {
            $request->error($e->getCode(), $e->getMessage());
        }

        continue;
    }
}
