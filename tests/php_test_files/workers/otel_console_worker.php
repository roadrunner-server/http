<?php

declare(strict_types=1);
ini_set('display_errors', 'stderr');
require dirname(__DIR__) . "/vendor/autoload.php";

use OpenTelemetry\API\Trace\Propagation\TraceContextPropagator;
use Spiral\RoadRunner;
use Nyholm\Psr7;
use OpenTelemetry\SDK\Registry;

use OpenTelemetry\SDK\Trace\SpanExporter\ConsoleSpanExporter;
use OpenTelemetry\SDK\Trace\SpanProcessor\SimpleSpanProcessor;
use OpenTelemetry\SDK\Trace\TracerProvider;

$worker = RoadRunner\Worker::create();
$psrFactory = new Psr7\Factory\Psr17Factory();

$tracerProvider =  new TracerProvider(
    new SimpleSpanProcessor(
        new ConsoleSpanExporter(Registry::transportFactory('stream')->create('php://stderr', 'application/json'))
    )
);

$tracer = $tracerProvider->getTracer('rr-http-test');
$worker = new RoadRunner\Http\PSR7Worker($worker, $psrFactory, $psrFactory, $psrFactory);

while ($req = $worker->waitRequest()) {
    try {
        $context = TraceContextPropagator::getInstance()->extract($req->getHeaders());
        $span = $tracer->spanBuilder('root')->setParent($context)->startSpan();
        $rsp = new Psr7\Response();
        $rsp->getBody()->write('Hello world!');

        $worker->respond($rsp);
        $span->end();
    } catch (\Throwable $e) {
        $worker->getWorker()->error((string)$e);
    }
}
