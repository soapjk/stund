all:
	go build -o stund ./main.go 
install:
	cp ./stund /usr/local/bin/
