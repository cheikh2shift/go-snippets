import { createServer } from "node:http";

const body = JSON.stringify({ message: "hello", status: "ok" });

const server = createServer((_req, res) => {
	res.writeHead(200, { "Content-Type": "application/json" });
	res.end(body);
});

server.listen(8080, () => console.log("ts :8080"));