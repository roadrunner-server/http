<?php

declare(strict_types=1);

require __DIR__ . '/vendor/autoload.php';

use Spiral\Goridge\Relay;
use Spiral\Goridge\RPC\RPC;

$rpc = new RPC(
    Relay::create('tcp://0.0.0.0:6001')
);
echo "foo";
$rpc = $rpc->withServicePrefix('metrics');

$rpc->call('Declare', [
    'name'      => 'test',
    'collector' => [
        'namespace' => 'foo',
        'subsystem' => 'bar',
        'type'      => 'counter',
        'help'      => '',
        'labels'    => [],
        'buckets'   => [],
    ],
]);

echo "foo2";
$rpc->call('Add', [
    'name'   => 'test',
    'value'  => 1.0,
    'labels' => [],
]);

echo "ON INIT";