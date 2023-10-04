<?php declare(strict_types=1);

use Nyholm\Psr7\Factory\Psr17Factory;
use Psr\Http\Message\ServerRequestInterface;
use Spiral\RoadRunner\Http\PSR7Worker;
use Spiral\RoadRunner\Worker;

include __DIR__ . '/vendor/autoload.php';

$factory = new Psr17Factory();
$rrWorker = Worker::create();
$worker = new PSR7Worker($rrWorker, $factory, $factory, $factory);
$pdo = null;
$log = $rrWorker->getLogger();
$start = microtime(true);

file_put_contents(__DIR__ . '/big-resp', str_repeat('R', 1024 * 1024 * 10), FILE_APPEND);

while (true) {
    $req = microtime(true);
    try {
        $psr = $worker->waitRequest();
        if (!$psr instanceof ServerRequestInterface) {
            break;
        }
    } catch (Throwable $e) {
        $worker->respond(new \Nyholm\Psr7\Response(200, [], "RR {$e->getMessage()}"));
        continue;
    }

    try {
        if (str_starts_with($psr->getUri()->getPath(), '/pdo')) {
            if (!$pdo) {
                $log->info('connect', [
                    'start' => microtime(true) - $start
                ]);
            }
            $buffer = file_get_contents(__DIR__ . '/well');
            $worker->respond(new \Nyholm\Psr7\Response(200, [], $buffer));
        } else {
            $log->info('empty', [
                'start' => microtime(true) - $start,
                'req' => microtime(true) - $req,
            ]);
            $worker->respond(new \Nyholm\Psr7\Response(200, [], "Empty"));
        }
    } catch (Throwable $e) {
        $worker->respond(new \Nyholm\Psr7\Response(200, [], "Err: {$e->getMessage()}"));
    }
}
