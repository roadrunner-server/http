<?php

use Spiral\RoadRunner;

ini_set("display_errors", "stderr");
require dirname(__DIR__) . "/vendor/autoload.php";

$http = new RoadRunner\Http\HttpWorker(RoadRunner\Worker::create());

while ($request = $http->waitRequest()) {
	if (str_contains($request->uri, "hint")) {
		$http->respond(
			103,
			"",
			["Link" => ["</a.css>; rel=preload"]],
			endOfStream: false,
		);
	}
	$http->respond(404, "body", ["X-Marker" => ["probe"]]);
}
