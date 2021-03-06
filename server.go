package main

import (
    "bytes"
    "fmt"
    "html/template"
    "encoding/json"
    //"image"
    "image/jpeg"
    "image/png"
    "io"
    //"io/ioutil"
    //"path"
    "log"
    "net"
    "net/http"
    "os"
    //"strings"
    "encoding/base64"
    "sync"
    "time"
    "os/exec"
    "bufio"
    "github.com/nfnt/resize"
    "github.com/gorilla/websocket"
    
    "go.nanomsg.org/mangos/v3"
	  // register transports
	  _ "go.nanomsg.org/mangos/v3/transport/all"
	  //uj "github.com/nanoscopic/ujsonin/mod"
)

func callback( r *http.Request ) bool {
    return true
}

var upgrader = websocket.Upgrader {
    CheckOrigin: callback,
}

type ImgMsg struct {
    imgNum int
    msg string
    data []byte
}

type ImgType struct {
    rawData []byte
}

const (
    WriterStop = iota
)

type WriterMsg struct {
    msg int
}

type MainMsg struct {
    msg int
}

type WdaResult struct {
    Value string `json:"value"`
    SessionId string `json:"sessionId"`
}

const (
    BeginDiscard = iota
    EndDiscard
)

type Stats struct {
    recv int
    dumped int
    sent int
    socketConnected bool
    waitCnt int
}

func startScreenshotServer( inSock mangos.Socket, stopChannel chan bool, mirrorPort string, wdaport string, tunName string, secure bool, cert string, key string, coordinator string, udid string ) {
    var err error
    
    ifaces, err := net.Interfaces()
    if err != nil {
        fmt.Printf( err.Error() )
        os.Exit( 1 )
    }
    
    var listen_addr string
    if tunName == "none" {
        fmt.Printf("No tunnel specified; listening on all interfaces\n")
        listen_addr = ":" + mirrorPort
    } else {
        foundInterface := false
        for _, iface := range ifaces {
            addrs, err := iface.Addrs()
            if err != nil {
                fmt.Printf( err.Error() )
                os.Exit( 1 )
            }
            for _, addr := range addrs {
                var ip net.IP
                switch v := addr.(type) {
                    case *net.IPNet:
                        ip = v.IP
                    case *net.IPAddr:
                        ip = v.IP
                    default:
                        fmt.Printf("Unknown type\n")
                }
                if iface.Name == tunName {
                    listen_addr = ip.String() + ":" + mirrorPort
                    foundInterface = true
                }
            }
        }
        if foundInterface == false {
            fmt.Printf( "Could not find interface %s\n", tunName )
            os.Exit( 1 )
        }
    }
  
    imgCh := make(chan ImgMsg, 60)
    mainCh := make( chan MainMsg, 4 )
    
    lock := sync.RWMutex{}
    var stats Stats = Stats{}
    stats.recv = 0
    stats.dumped = 0
    stats.socketConnected = false
    
    statLock := sync.RWMutex{}
    
    sentSize := false
    discard := true

    /*
    // create cache folder for screenshots
    dirname := "./cache/" + udid
    _ = os.Mkdir(dirname, os.ModePerm)
    */

    go func() {
        imgnum := 1
        
        wdaTimeoutCount := 0
        wdaTimeoutDelay := 3

        LOOP:
        for {
            select {
                case <- stopChannel:
                    fmt.Printf("Server channel got stop message\n")
                    break LOOP
                case msg := <- mainCh:
                    if msg.msg == BeginDiscard {
                        discard = true
                    } else if msg.msg == EndDiscard {
                        discard = false
                    } 
                default: // this makes the above read from stopChannel non-blocking
            }

            if discard && sentSize {
                statLock.Lock()
                stats.dumped++
                statLock.Unlock()

                continue
            }
            
            if !discard {
                /*
                // find cache folder
                dir, err := ioutil.ReadDir(dirname)
                if err != nil {
                    fmt.Printf(err.Error())
                }

                // remove all previous screenshots
                for _, d := range dir {
                    os.RemoveAll(path.Join([]string{dirname, d.Name()}...))
                }

                // take screenshot of the device
                cmd := exec.Command("idevicescreenshot", "-u", udid)
                cmd.Dir = dirname
                _, err = cmd.Output()
                if err != nil {
                    fmt.Printf(err.Error())
                }

                // get screenshot filename
                files, err := ioutil.ReadDir(dirname)
                if err != nil {
                    fmt.Printf(err.Error())
                }
                if len(files) == 0 {
                    fmt.Printf("No screenshot in folder")
                    continue
                }
                filename := files[len(files)-1].Name()
                fmt.Printf("%s\n", filename)
                
                // get screenshot file
                infile, err := os.Open(dirname + "/" + filename)
                if err != nil {
                    fmt.Printf("fileerror: %s", err.Error())
                    panic(err.Error())
                }

                // decode file to image
                src, _, err := image.Decode(infile)
                infile.Close()
                if err != nil {
                    fmt.Printf("imgerror: %s", err.Error())
                    panic(err.Error())
                }

                // encode image to bytes
                buf := new(bytes.Buffer)
                err = png.Encode(buf, src)
                if err != nil {
                    fmt.Printf("pngerror: %s", err.Error())
                    panic(err.Error())
                }
                */
                
                imgPull := make(chan string, 1)

                if wdaTimeoutCount % 50 == 0 {
                    wdaTimeoutDelay = 3
                } else {
                    wdaTimeoutDelay = 0
                }

                if (wdaTimeoutDelay == 3) {
                    go func() {
                        cmdwda := exec.Command("curl", "http://localhost:" + wdaport + "/screenshot")
                        stdoutwda, err := cmdwda.StdoutPipe()
                        if err != nil {
                            fmt.Printf("Can't read from stdout curl: %s\n", err.Error())
                            //panic(err.Error())
                            time.Sleep(3 * time.Second)
                            return
                        }
    
                        // read command's stdout line by line
                        inwda := bufio.NewReader(stdoutwda)
                
                        // start the command after having set up the pipe
                        err = cmdwda.Start()
                        if err != nil {
                            fmt.Printf("Exec start error: %s\n", err.Error())
                            //panic(err.Error())
                            time.Sleep(3 * time.Second)
                            return
                        }
    
                        //stdin.Write([]byte("i\n"))
    
                        var (output []byte
                            errreadline error = nil
                        )
                        for {
                            var (isPrefix bool = true
                                line []byte
                            )
                            for isPrefix && errreadline == nil {
                                line, isPrefix, errreadline = inwda.ReadLine()
                                //fmt.Printf("line %s\n", line)
                                output = append(output, line...)
                            }
                            
                            if errreadline != nil {
                                break
                            }
                        }
    
                        if errreadline != io.EOF {
                            fmt.Printf("errreadline: %s\n", errreadline.Error())
                            //panic(err.Error())
                            time.Sleep(3 * time.Second)
                            return
                        }
    
                        if err := cmdwda.Wait(); err != nil {
                            fmt.Printf("cmdwda wait error: %s\n", err.Error())
                            //panic(err.Error())
                            time.Sleep(3 * time.Second)
                            return
                        }
    
                        wdaresult := WdaResult{}
                        
                        json.Unmarshal(output, &wdaresult)
                        
                        if wdaresult.Value == "" {
                            fmt.Printf("wda image fail\n")
                            time.Sleep(3 * time.Second)
                        } else {
                            imgPull <- wdaresult.Value
                        }

                    }()
                }
                
                var s string
                select {
                case img := <-imgPull:
                    fmt.Printf("Got wda image\n")
                    s = string(img)
                case <-time.After(time.Duration(wdaTimeoutDelay) * time.Second):
                    fmt.Printf("wda timeout\n")
                    wdaTimeoutCount++

                    cmd := exec.Command("./repos/libimobiledevice/tools/idevicescreenshot", "-o", "-u", udid)
                    stdout, err := cmd.StdoutPipe()
                    if err != nil {
                        fmt.Printf("Can't read from stdout idevicescreenshot: %s\n", err.Error())
                        //panic(err.Error())
                        time.Sleep(3 * time.Second)
                        return
                    }

                    // read command's stdout line by line
                    in := bufio.NewReader(stdout)

                    // start the command after having set up the pipe
                    err = cmd.Start()
                    if err != nil {
                        fmt.Printf("Exec start error: %s\n", err.Error())
                        //panic(err.Error())
                        time.Sleep(3 * time.Second)
                        return
                    }

                    var (output []byte
                        errreadline error = nil
                        isPrefix bool = true
                        line []byte
                    )
                    for isPrefix && errreadline == nil {
                        line, isPrefix, errreadline = in.ReadLine()
                        //fmt.Printf("line %c%c%c%c%c\n", line[0], line[1], line[2], line[3], line[4])
                        fmt.Printf("line\n")
                        output = append(output, line...)
                    }
                    if errreadline != nil {
                        fmt.Printf("errreadline: %s\n", errreadline.Error())
                        //panic(err.Error())
                        time.Sleep(3 * time.Second)
                        continue
                    }
                    if err := cmd.Wait(); err != nil {
                        fmt.Printf("cmd wait error: %s\n", err.Error())
                        //panic(err.Error())
                        time.Sleep(3 * time.Second)
                        return
                    }

                    fmt.Printf("Got ids image\n")
                    s = string(output)
                }

                unbased, err := base64.StdEncoding.DecodeString(s)
                if err != nil {
                    fmt.Printf("Decode b64 image error: %\n", err.Error())
                    panic(err.Error())
                    time.Sleep(3 * time.Second)
                    continue
                }
                
                // decode file to image
                src, err := png.Decode(bytes.NewReader(unbased))
                if err != nil {
                    fmt.Printf("Decode image error: %s\n", err.Error())
                    panic(err.Error())
                }

                resized := resize.Resize(500, 0, src, resize.Lanczos3)

                var options jpeg.Options
                options.Quality = 30

                // encode image to bytes
                buf := new(bytes.Buffer)
                err = jpeg.Encode(buf, resized, &options)
                if err != nil {
                    fmt.Printf("Encode image error: %s\n", err.Error())
                    panic(err.Error())
                }
                
                // send screenshot to websocket
                imgMsg := ImgMsg{}
                imgMsg.data = buf.Bytes()
                imgMsg.imgNum = imgnum
                imgCh <- imgMsg
                
            } else {
                time.Sleep(3 * time.Second)
            }

            statLock.Lock()
            stats.recv++
            statLock.Unlock()

            imgnum++
        }
    }()
    
    startServer( imgCh, mainCh, &lock, &statLock, &stats, listen_addr, secure, cert, key )
}

func startWriter(ws *websocket.Conn,imgCh <-chan ImgMsg,writerCh <-chan WriterMsg,lock *sync.RWMutex, statLock *sync.RWMutex, stats *Stats) {
    go func() {
        var running bool = true
        statLock.Lock()
        stats.socketConnected = true
        statLock.Unlock()
        for {
            select {
                case imgMsg := <- imgCh:
                    // Keep receiving images till there are no more to receive
                    stop := false
                    for {
                        select {
                        case imgMsg = <- imgCh:
                            
                        default:
                            stop = true
                        }
                        if stop { break }
                    }
            
                    msg := []byte(imgMsg.msg)
                    imgBytes := imgMsg.data
                    
                    lock.Lock()
                    ws.WriteMessage(websocket.TextMessage, msg)
                    ws.WriteMessage(websocket.BinaryMessage, imgBytes )
                    lock.Unlock()
                case controlMsg := <- writerCh:
                    if controlMsg.msg == WriterStop {
                        running = false
                    }
            }
            if running == false {
                break
            }
        }
        statLock.Lock()
        stats.socketConnected = false
        statLock.Unlock()
    }()
}

func startServer( imgCh <-chan ImgMsg, mainCh chan<- MainMsg, lock *sync.RWMutex, statLock *sync.RWMutex, stats *Stats, listen_addr string, secure bool, cert string, key string ) (*http.Server) {
    fmt.Printf("Listening on %s\n", listen_addr )
    
    echoClosure := func( w http.ResponseWriter, r *http.Request ) {
        handleEcho( w, r, imgCh, mainCh, lock, statLock, stats )
    }
    statsClosure := func( w http.ResponseWriter, r *http.Request ) {
        handleStats( w, r, statLock, stats )
    }
    rootClosure := func( w http.ResponseWriter, r *http.Request ) {
        handleRoot( w, r, secure )
    }
    
    server := &http.Server{ Addr: listen_addr }
    http.HandleFunc( "/echo", echoClosure )
    http.HandleFunc( "/echo/", echoClosure )
    http.HandleFunc( "/", rootClosure )
    http.HandleFunc( "/stats", statsClosure )
    go func() {
        if secure {
            server.ListenAndServeTLS( cert, key );
        } else {
            server.ListenAndServe()
        }
    }()
    return server
}

func handleStats( w http.ResponseWriter, r *http.Request, statLock *sync.RWMutex, stats *Stats ) {
    statLock.Lock()
    recv := stats.recv
    dumped := stats.dumped
    socketConnected := stats.socketConnected
    statLock.Unlock()
    waitCnt := stats.waitCnt
    
    var socketStr string = "no"
    if socketConnected {
        socketStr = "yes"
    }
    
    fmt.Fprintf( w, "Received: %d<br>\nDumped: %d<br>\nSocket Connected: %s<br>\nWait Count: %d<br>\n", recv, dumped, socketStr, waitCnt )
}

func handleEcho( w http.ResponseWriter, r *http.Request,imgCh <-chan ImgMsg,mainCh chan<- MainMsg, lock *sync.RWMutex, statLock *sync.RWMutex, stats *Stats ) {
    c, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Print("Upgrade error:", err)
        return
    }
    defer c.Close()
    fmt.Printf("Received connection\n")
    welcome(c)
        
    writerCh := make(chan WriterMsg, 2)
    
    // stop discarding images
    mainCh <- MainMsg{ msg: EndDiscard }
    
    stopped := false
    
    c.SetCloseHandler( func( code int, text string ) error {
        stopped = true
        writerCh <- WriterMsg{ msg: WriterStop }
        mainCh   <- MainMsg{ msg: BeginDiscard }
        return nil
    } )
    
    
    startWriter(c,imgCh,writerCh,lock,statLock,stats)
    for {
        mt, message, err := c.ReadMessage()
        if err != nil {
            log.Println("read:", err)
            break
        }
        log.Printf("recv: %s", message)
        lock.Lock()
        err = c.WriteMessage(mt, message)
        lock.Unlock()
        if err != nil {
            log.Println("write:", err)
            break
        }
    }
    
    // send WriterMsg to terminate writer
    if !stopped {
        writerCh <- WriterMsg{ msg: WriterStop }
        mainCh   <- MainMsg{ msg: BeginDiscard }
    }
}

func welcome( c *websocket.Conn ) ( error ) {
    msg := `
{
    "version":1,
    "length":24,
    "pid":12733,
    "realWidth":750,
    "realHeight":1334,
    "virtualWidth":375,
    "virtualHeight":667,
    "orientation":0,
    "quirks":{
        "dumb":false,
        "alwaysUpright":true,
        "tear":false
    }
}`
    return c.WriteMessage( websocket.TextMessage, []byte(msg) )
}

func handleRoot( w http.ResponseWriter, r *http.Request, secure bool ) {
    if secure {
        rootTpl.Execute( w, "wss://"+r.Host+"/echo" )
    } else {
        rootTpl.Execute( w, "ws://"+r.Host+"/echo" )
    }
}

var rootTpl = template.Must(template.New("").Parse(`
<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<style>
  canvas {
    border: solid 1px black;
  }
</style>
<script>
  function getel( id ) {
    return document.getElementById( id );
  }
  function getCursorPosition(canvas, event) {
    const rect = canvas.getBoundingClientRect()
    const x = event.clientX - rect.left
    const y = event.clientY - rect.top
    console.log("x: " + x + " y: " + y)
    return [x,y];
  }
  var session='';
  var wid=0;
  var heg=0;
  function req( type, url, handler, body ) {
    var xhr = new XMLHttpRequest();
    xhr.open( type, url );
    xhr.responseType = 'json';
    xhr.onload = function(x) { handler(x,xhr); }
    if( type == 'POST' && body ) xhr.send(body);
    else xhr.send();
  }
  function clickAt( pos ) {
    req( 'POST', 'http://localhost:8100/session/' + session + '/wda/tap/0', function() {}, JSON.stringify( { x: pos[0]/(1080/2)*wid, y: pos[1]/(1920/2)*heg } ) );
  }
  window.addEventListener("load", function(evt) {
    var output = getel("output");
    var input  = getel("input");
    var canvas = getel("canvas");
    var ctx    = canvas.getContext("2d");
    var ws;
    
    canvas.onclick = function( event ) {
      var pos = getCursorPosition( canvas, event );
      if( !session ) {
        req( 'GET', 'http://localhost:8100/status', function( a,xhr ) {
          session = xhr.response.sessionId;
          req( 'GET', 'http://localhost:8100/session/'+session+'/window/size', function( a,xhr ) {
            wid = xhr.response.value.width;
            heg = xhr.response.value.height;
            //console.log( xhr.response );
            clickAt( pos )
          } );
          
        } );
      } else {
        clickAt( pos );
      }
    }
    getel("open").onclick = function( event ) {
      if( ws ) {
        return false;
      }
      ws = new WebSocket("{{.}}");
      ws.onopen = function( event ) {
        console.log("Websocket open");
      }
      ws.onclose = function( event ) {
        console.log("Websocket closed");
        ws = null;
      }
      ws.onmessage = function( event ) {
        if( event.data instanceof Blob ) {
          var image = new Image();
          var url;
          image.onload = function() {
            ctx.drawImage(image, 0, 0);
            URL.revokeObjectURL( url );
          };
          image.onerror = function( e ) {
            console.log('Error during loading image:', e);
          }
          var blob = event.data;
          
          url = URL.createObjectURL( blob );
          image.src = url;
        }
        else {
          var text = "Response: " + event.data;
          if( event.data != 'none' ) {
            var d = document.createElement("div");
            d.innerHTML = text;
            output.appendChild( d );
          }
        }
      }
      ws.onerror = function( event ) {
        console.log( "Error: ", event.data );
      }
      return false;
    };
    getel("send").onclick = function( event ) {
      if( !ws ) return false;
      ws.send( input.value );
      return false;
    };
    getel("close").onclick = function( event)  {
      if(!ws) return false;
      ws.close();
      return false;
    };
  });
</script>
</head>
<body>
  <table>
    <tr>
      <td valign="top">
        <canvas id="canvas" width="750" height="1334"></canvas>
      </td>
      <td valign="top" width="50%">
        <form>
          <button id="open">Open</button>
          <button id="close">Close</button>
          <br>
          <input id="input" type="text" value="">
          <button id="send">Send</button>
        </form>
      </td>
      <td valign="top" width="50%">
        <div id="output"></div>
      </td>
    </tr>
  </table>
</body>
</html>
`))