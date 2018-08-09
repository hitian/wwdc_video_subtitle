package main

import (
	"flag"
	"fmt"
	"golang.org/x/net/html"
	"io/ioutil"
	"net/http"
	"os"
	"regexp"
	"strings"
)

const (
	baseURL = "https://developer.apple.com"
)

var langList = []string{"zho", "eng"}
var videoQuality = []string{"hd", "sd"}

var client = &http.Client{}

func main() {
	var year, lang, quality string
	flag.StringVar(&year, "year", "2017", "year")
	flag.StringVar(&lang, "lang", "eng", "language: "+strings.Join(langList, ", "))
	flag.StringVar(&quality, "quality", "hd", "video quality: "+strings.Join(videoQuality, ", "))
	flag.Parse()

	if year == "" {
		panic("year required")
	}
	if index(langList, lang) == -1 {
		panic("lang error")
	}

	fmt.Println("fetch session list for year ", year)

	sessionURLs := getSessionURLsByMainPage(year)
	if len(sessionURLs) < 1 {
		panic("can not found any session's URL")
	}

	sessionTotalCount := len(sessionURLs)
	fmt.Println("found session total number: ", sessionTotalCount)

	i := 1
	videoURLs := make([]string, 0)
	pdfURLs := make([]string, 0)
	for _, sessionURL := range sessionURLs {
		fmt.Printf("fetching session page (%d/%d)\n", i, sessionTotalCount)
		sessionDetail := getSessionDetailPage(sessionURL)
		targetVideoURL := sessionDetail.hdURL
		if quality == "sd" {
			targetVideoURL = sessionDetail.sdURL
		}
		getSubtitle(targetVideoURL, year, lang)
		if len(targetVideoURL) > 5 {
			videoURLs = append(videoURLs, targetVideoURL)
		}
		if len(sessionDetail.pdfURL) > 5 {
			pdfURLs = append(pdfURLs, sessionDetail.pdfURL)
		}
		i++
	}

	//write video url to file
	ioutil.WriteFile("videoURLs"+year+".txt", []byte(strings.Join(videoURLs, "\n")), 0755)
	ioutil.WriteFile("pdfURLs"+year+".txt", []byte(strings.Join(pdfURLs, "\n")), 0755)

	fmt.Println("done!")
}

func getSessionURLsByMainPage(year string) []string {
	var sessionURLs = []string{}
	pageURL := baseURL + "/videos/wwdc" + year + "/?"
	fmt.Println("fetch page: " + pageURL)
	resp, err := requestWithUA(pageURL)
	if err != nil || resp.StatusCode != http.StatusOK {
		fmt.Printf("Error: %v, responseCode: %d \n", err, resp.StatusCode)
		panic("get session list page error")
	}
	defer resp.Body.Close()
	z := html.NewTokenizer(resp.Body)

	sessionIDRegexp := regexp.MustCompile(`^/videos/play/wwdc([0-9]+)/([0-9]+)/$`)
	for {
		node := z.Next()
		if node == html.ErrorToken {
			break
		} else if node == html.StartTagToken {
			tag := z.Token()
			isATag := tag.Data == "a"
			if !isATag {
				continue
			}

			for _, v := range tag.Attr {
				if v.Key == "href" {
					matched := sessionIDRegexp.FindStringSubmatch(v.Val)
					if len(matched) == 3 {
						if index(sessionURLs, matched[0]) == -1 {
							sessionURLs = append(sessionURLs, matched[0])
						}
					}
					break
				}
			}
		}
	}
	return sessionURLs
}

func getSessionDetailPage(URL string) sessionDetails {
	pageURL := baseURL + URL
	fmt.Println("fetch video page: " + pageURL)
	resp, err := requestWithUA(pageURL)
	if err != nil || resp.StatusCode != http.StatusOK {
		fmt.Printf("Error: %v, responseCode: %d \n", err, resp.StatusCode)
		panic("get video page error")
	}
	defer resp.Body.Close()

	page := html.NewTokenizer(resp.Body)
	videoURLRegex := regexp.MustCompile(`http(.+).apple.com/videos/wwdc/([0-9]{4})/(.+)/(\d+)/(.+).mp4\?dl=1`)
	pdfURLRegex := regexp.MustCompile(`http(.+).apple.com/videos/wwdc/([0-9]{4})/(.+)/(\d+)/(.+).pdf`)

	var details = sessionDetails{}

	for {
		node := page.Next()
		if node == html.ErrorToken {
			break
		} else if node == html.StartTagToken {
			tag := page.Token()
			isATag := tag.Data == "a"
			if !isATag {
				continue
			}
			for _, v := range tag.Attr {
				if v.Key == "href" {
					matched := videoURLRegex.FindStringSubmatch(v.Val)
					if len(matched) > 0 {
						details.id = matched[4]
						filename := matched[5]
						if strings.HasPrefix(filename, details.id+"_hd_") {
							details.hdURL = matched[0]
							details.hdName = filename
						} else {
							details.sdURL = matched[0]
							details.sdName = filename
						}
						continue
					}
					matchedPDF := pdfURLRegex.FindStringSubmatch(v.Val)
					if len(matchedPDF) > 0 {
						if details.id == "" {
							details.id = matchedPDF[4]
						}
						details.pdfName = matchedPDF[5]
						details.pdfURL = matchedPDF[0]
					}
				}
			}
		}
	}

	return details
}

type sessionDetails struct {
	id, hdURL, hdName, sdURL, sdName, pdfURL, pdfName string
}

func getSubtitle(videoURL, year, lang string) {
	if len(videoURL) < 10 {
		fmt.Println("session video not exist, skip.")
		return
	}
	baseURL := videoURL[:strings.LastIndex(videoURL, "/")]
	subtitleBaseURL := baseURL + "/subtitles/" + lang + "/"
	subtitleIndexURL := subtitleBaseURL + "prog_index.m3u8"
	fmt.Println("subtitleIndexURL: ", subtitleIndexURL)

	//save path
	//fmt.Println("video URL: "+videoURL, strings.LastIndex(videoURL, "/"), strings.Index(videoURL, "."))
	videoName := videoURL[strings.LastIndex(videoURL, "/")+1:]
	videoName = videoName[:strings.Index(videoName, ".")]
	subtitleSaveDir := "subtitles" + year
	if _, err := os.Stat(subtitleSaveDir); os.IsNotExist(err) {
		err = os.MkdirAll(subtitleSaveDir, 0755)
		if err != nil {
			panic("create dir failed")
		}
	}
	filePath := subtitleSaveDir + string(os.PathSeparator) + videoName + "." + lang + ".srt"
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		fmt.Println("file exist, skip.")
		return
	}
	fmt.Println("start fetch subtitle: " + videoName)
	indexContent := getURLContent(subtitleIndexURL)
	webvttFiles := []string{}
	for _, line := range strings.Split(indexContent, "\n") {
		if strings.HasSuffix(line, ".webvtt") {
			webvttFiles = append(webvttFiles, line)
		}
	}

	//start get content.
	var text = []string{}
	var count = 1
	timeRegex := regexp.MustCompile(`([0-9]+):([0-9]+):([0-9]+)\.([0-9]+).+([0-9]+):([0-9]+):([0-9]+)\.([0-9]+).*`)
	for _, fileSequence := range webvttFiles {
		sequenceURL := subtitleBaseURL + fileSequence
		sequenceContent := getURLContent(sequenceURL)
		var isStart = false
		var isTimePushed = false
		for _, line := range strings.Split(sequenceContent, "\n") {
			if !isStart && !strings.Contains(line, "-->") {
				continue
			}
			if strings.Contains(line, "-->") {
				timeMatched := timeRegex.FindStringSubmatch(line)
				// fmt.Printf("%#v\n", timeMatched)
				if len(timeMatched) > 0 {
					text = append(text, fmt.Sprintf("%d", count))
					timeList := timeMatched[1:]
					is := make([]interface{}, len(timeList))
					for i, v := range timeList {
						is[i] = v
					}
					text = append(text, fmt.Sprintf("%s:%s:%s,%s --> %s:%s:%s,%s", is...))
				} else {
					panic("time can not parse. :" + line)
				}
				//panic("er")
				isTimePushed = true
				isStart = true
				count++
				continue
			}
			if isTimePushed {
				text = append(text, line)
				text = append(text, "")
				// fmt.Printf("%s\n", line)
				isTimePushed = false
				continue
			}
			// fmt.Println("rest: " + line)
		}
	}

	ioutil.WriteFile(filePath, []byte(strings.Join(text, "\n")), 0755)

}

func getURLContent(URL string) string {
	resp, err := http.Get(URL)
	if err != nil {
		panic("get url content error: " + URL)
	}
	html, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		panic("url content read error:" + URL)
	}
	return string(html)
}

func index(vs []string, t string) int {
	for i, v := range vs {
		if v == t {
			return i
		}
	}
	return -1
}

func requestWithUA(URL string) (*http.Response, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", URL, nil)
	if err != nil {
		panic("build request failed")
	}
	req.Header.Set("accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_13_0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/61.0.3163.100 Safari/537.36")
	req.Header.Set("accept-language", "zh-CN,zh;q=0.8,en-US;q=0.6,en;q=0.4,zh-TW;q=0.2")

	resp, err := client.Do(req)
	return resp, err
}
