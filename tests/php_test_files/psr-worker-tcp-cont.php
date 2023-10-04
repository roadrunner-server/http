<?php

require __DIR__ . '/vendor/autoload.php';

use Spiral\RoadRunner\Worker;
use Spiral\RoadRunner\Tcp\TcpWorker;
use Spiral\RoadRunner\Tcp\TcpResponse;
use Spiral\RoadRunner\Tcp\TcpEvent;

// Create new RoadRunner worker from global environment
$worker = Worker::create();

$tcpWorker = new TcpWorker($worker);

while ($request = $tcpWorker->waitRequest()) {
    if (is_null($request)) {
            return;
    }

    try {
        if ($request->event === TcpEvent::Connected) {
            // -----------------

            // Or send response to the TCP connection, for example, to the SMTP client
            $tcpWorker->respond("hello \r\n");
        } elseif ($request->event === TcpEvent::Data) {

            if ($request->server === 'server1') {
                // Send response to the TCP connection and wait for the next request
                $tcpWorker->respond(json_encode([
                    'body' => $request->body,
                    'uuid' => $request->connectionUuid,
                    'remote_addr' => "foo1",
                ]));
            } elseif ($request->server === 'server2') {

                // Send response to the TCP connection and wait for the next request
                $tcpWorker->respond(json_encode([
                    'body' => $request->body,
                    'remote_addr' => "foo2",
                ]));
            } elseif (($request->server === 'server3')) {
                // Send response to the TCP connection and wait for the next request
                $tcpWorker->respond(json_encode([
                    'body' => $request->body,
                    'remote_addr' => "foo3",
                ]));
            }
            // Handle closed connection event
        } elseif ($request->event === "CLOSE") {
                // Send response to the TCP connection and wait for the next request
                $tcpWorker->respond(json_encode([
                    'body' => $request->body,
                    'remote_addr' => "foo3",
                ]));
        }
    } catch (\Throwable $e) {
	$tcpWorker->respond("Something went wrong\r\n", TcpResponse::RespondClose);
        $worker->error((string)$e);
    }
}
