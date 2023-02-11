run:
	docker run --rm --name esm-cdn -p 80:80 esm-cdn
build1:
	docker build --platform linux/amd64 -f Dockerfile.local -t esm-cdn .
build2:
	docker build --platform linux/amd64 -f Dockerfile -t esm-cdn2 .

run2:
	docker run --rm --name esm-cdn2 -p 8080:8080 esm-cdn2
