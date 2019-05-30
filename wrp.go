//
// WRP - Web Rendering Proxy
//
// Copyright (c) 2013-2018 Antoni Sawicki
// Copyright (c) 2019 Google LLC
//

package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	_ "image"
	"image/gif"
	"image/png"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/chromedp/cdproto/emulation"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/chromedp"
)

var (
	ctx    context.Context
	cancel context.CancelFunc
	gifmap = make(map[string]bytes.Buffer)
)

func pageServer(out http.ResponseWriter, req *http.Request) {
	req.ParseForm()
	furl := req.Form["url"]
	var gourl string
	if len(furl) >= 1 && len(furl[0]) > 4 {
		gourl = furl[0]
	} else {
		gourl = "https://www.bbc.com/news"
	}
	log.Printf("%s Page Reqest for %s [%s]\n", req.RemoteAddr, gourl, req.URL.Path)
	out.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(out, "<HTML>\n<HEAD><TITLE>WRP %s</TITLE>\n<BODY BGCOLOR=\"#F0F0F0\">", gourl)
	fmt.Fprintf(out, "<FORM ACTION=\"/\">URL: <INPUT TYPE=\"TEXT\" NAME=\"url\" VALUE=\"%s\">", gourl)
	fmt.Fprintf(out, "<INPUT TYPE=\"SUBMIT\" VALUE=\"Go\"></FORM><P>\n")
	if len(gourl) > 4 {
		capture(gourl, out)
	}
	fmt.Fprintf(out, "</BODY>\n</HTML>\n")
}

func imgServer(out http.ResponseWriter, req *http.Request) {
	log.Printf("%s Img Request for %s\n", req.RemoteAddr, req.URL.Path)
	gifbuf := gifmap[req.URL.Path]
	defer delete(gifmap, req.URL.Path)
	out.Header().Set("Content-Type", "image/gif")
	out.Header().Set("Content-Length", strconv.Itoa(len(gifbuf.Bytes())))
	out.Write(gifbuf.Bytes())
	out.(http.Flusher).Flush()
}

func haltServer(out http.ResponseWriter, req *http.Request) {
	log.Printf("%s Shutdown request received [%s]\n", req.RemoteAddr, req.URL.Path)
	out.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(out, "WRP Shutdown")
	out.(http.Flusher).Flush()
	cancel()
	os.Exit(0)
}

func capture(gourl string, out http.ResponseWriter) {
	var nodes []*cdp.Node
	ctxx := chromedp.FromContext(ctx)
	var pngbuf []byte
	var gifbuf bytes.Buffer
	var loc string

	log.Printf("Processing Caputure Request for %s\n", gourl)

	// Run ChromeDP Magic
	chromedp.Run(ctx,
		emulation.SetDeviceMetricsOverride(1024, 768, 1.0, false),
		chromedp.Navigate(gourl),
		chromedp.Sleep(time.Second*2),
		chromedp.CaptureScreenshot(&pngbuf),
		chromedp.Location(&loc),
		chromedp.Nodes("a", &nodes, chromedp.ByQueryAll))

	log.Printf("Landed on: %s, Got %d nodes\n", loc, len(nodes))

	// Process Screenshot Image
	img, err := png.Decode(bytes.NewReader(pngbuf))
	if err != nil {
		log.Printf("Failed to decode screenshot: %s\n", err)
		fmt.Fprintf(out, "<BR>Unable to decode page screenshot:<BR>%s<BR>\n", err)
		return
	}
	gifbuf.Reset()
	err = gif.Encode(&gifbuf, img, nil)
	if err != nil {
		log.Printf("Failed to encode GIF: %s\n", err)
		fmt.Fprintf(out, "<BR>Unable to encode GIF:<BR>%s<BR>\n", err)
		return
	}
	imgpath := fmt.Sprintf("/img/%04d.gif", rand.Intn(9999))
	gifmap[imgpath] = gifbuf

	// Process Nodes
	base, _ := url.Parse(loc)
	fmt.Fprintf(out, "<IMG SRC=\"%s\" ALT=\"wrp\" USEMAP=\"#map\">\n<MAP NAME=\"map\">\n", imgpath)
	log.Printf("Image path will be: %s", imgpath)

	for _, n := range nodes {
		b, err := dom.GetBoxModel().WithNodeID(n.NodeID).Do(cdp.WithExecutor(ctx, ctxx.Target))
		if err != nil {
			continue
		}
		tgt, err := base.Parse(n.AttributeValue("href"))
		if err != nil {
			continue
		}
		target := fmt.Sprintf("/?url=%s", tgt)

		if len(b.Content) > 6 && len(target) > 7 {
			fmt.Fprintf(out, "<AREA SHAPE=\"RECT\" COORDS=\"%.f,%.f,%.f,%.f\" ALT=\"%s\" TITLE=\"%s\" HREF=\"%s\">\n",
				b.Content[0], b.Content[1], b.Content[4], b.Content[5], n.AttributeValue("href"), n.AttributeValue("href"), target)
		}
	}

	fmt.Fprintf(out, "</MAP>\n")
	out.(http.Flusher).Flush()
	log.Printf("Done with caputure for %s\n", gourl)
}

func main() {
	ctx, cancel = chromedp.NewContext(context.Background())
	defer cancel()
	var addr string
	flag.StringVar(&addr, "l", ":8080", "Listen address:port, default :8080")
	flag.Parse()
	rand.Seed(time.Now().UnixNano())
	http.HandleFunc("/", pageServer)
	http.HandleFunc("/img/", imgServer)
	http.HandleFunc("/favicon.ico", http.NotFound)
	http.HandleFunc("/halt", haltServer)
	log.Printf("Starting http server on %s\n", addr)
	http.ListenAndServe(addr, nil)
}