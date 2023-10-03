<?php
/**
 * @var Goridge\RelayInterface $relay
 */
use Spiral\Goridge;
use Spiral\RoadRunner;

ini_set('display_errors', 'stderr');
require __DIR__ . "/vendor/autoload.php";

$worker = new RoadRunner\Worker(new Goridge\StreamRelay(STDIN, STDOUT));
$httpWorker = new RoadRunner\Http\HttpWorker(
    $worker,
);

while ($req = $httpWorker->waitRequest()) {
    try {
        $httpWorker->respond(610, '', []);
    } catch (\Throwable $e) {
        $httpWorker->getWorker()->error((string)$e);
    }
}