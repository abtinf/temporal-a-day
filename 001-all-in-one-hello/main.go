package main

func main() {
	go servermain()
	go workermain()
	go clientmain()
	select {}
}
