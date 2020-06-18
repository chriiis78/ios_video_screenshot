TARGET = ios_video_screenshot

all: $(TARGET)

$(TARGET): main.go server.go go.sum
	go build -o $(TARGET) .

go.sum:
	go get
	go get .

clean:
	$(RM) $(TARGET) go.sum
