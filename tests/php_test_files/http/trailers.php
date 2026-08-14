<?php

use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;

function handleRequest(ServerRequestInterface $req, ResponseInterface $resp): ResponseInterface
{
    $resp->getBody()->write("checksummed");

    // Trailer announces which header RoadRunner has to move behind the body
    return $resp->withHeader('Trailer', 'X-Checksum')->withHeader('X-Checksum', 'abc');
}
