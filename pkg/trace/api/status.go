package api

import (
	"bytes"
	"encoding/json"
	"expvar"
	"fmt"
	"html/template"
	"net/http"

	"github.com/DataDog/datadog-agent/pkg/config"
	"github.com/dustin/go-humanize"
)

var statusTmpl = template.Must(template.New("status").Funcs(template.FuncMap{
	"humanize": humanize.Commaf,
	"percent":  func(v float64) string { return fmt.Sprintf("%02.1f", v*100) },
}).Parse(`
{{- if .error }}
  Status: Not running or unreachable on localhost:{{.port}}.<br>
  Error: {{.error}}<br>
{{else}}
  Status: Running<br>
  Pid: {{.pid}}<br>
  Uptime: {{.uptime}} seconds<br>
  Mem alloc: {{humanize .memstats.Alloc}} bytes<br>
  Hostname: {{.config.Hostname}}<br>
  Receiver: {{.config.ReceiverHost}}:{{.config.ReceiverPort}}<br>
  Endpoints:
  <span class="stat_subdata">
    {{- range $i, $e := .config.Endpoints}}
    {{ $e.Host }}<br>
    {{- end }}
  </span>
  <span class="stat_subtitle">Receiver (previous minute)</span>
  <span class="stat_subdata">
    {{- if .receiver -}}
    {{- if eq (len .receiver) 0}}
    No traces received in the previous minute.<br>
    {{- end -}}
    {{range $i, $ts := .receiver }}
    From {{if $ts.Lang}}{{ $ts.Lang }} {{ $ts.LangVersion }} ({{ $ts.Interpreter }}), client {{ $ts.TracerVersion }}{{else}}unknown clients{{end}}<br>
    <span class="stat_subdata">
      Traces received: {{ $ts.TracesReceived }} ({{ humanize $ts.TracesBytes }} bytes)<br>
      Spans received: {{ $ts.SpansReceived }}
      {{ with $ts.WarnString }}
      <br>WARNING: {{ . }}<br>
      {{end}}
    </span>
    {{- end}}
    {{- else -}}
    Nothing received.<br />
    {{- end -}}
    {{range $key, $value := .ratebyservice -}}
    {{- if eq $key "service:,env:" -}}
    Default priority sampling rate: {{percent $value}}%
    {{- else}}
    Priority sampling rate for '{{ $key }}': {{percent $value}}%
    {{- end}}
    {{- end }}
    {{- if lt .ratelimiter.TargetRate 1.0}}
    <br>WARNING: Rate-limiter keep percentage: {{percent .ratelimiter.TargetRate}}%<br>
    {{- end}}
  </span>
  <span class="stat_subtitle">Writer (previous minute)</span>
  <span class="stat_subdata">
    Traces: {{.trace_writer.Payloads}} payloads, {{.trace_writer.Traces}} traces, {{.trace_writer.Events}} events, {{.trace_writer.Bytes}} bytes<br>
    {{- if gt .trace_writer.Errors 0.0}}WARNING: Traces API errors (1 min): {{.trace_writer.Errors}}{{end}}
    Stats: {{.stats_writer.Payloads}} payloads, {{.stats_writer.StatsBuckets}} stats buckets, {{.stats_writer.Bytes}} bytes<br>
    {{- if gt .stats_writer.Errors 0.0}}WARNING: Stats API errors (1 min): {{.stats_writer.Errors}}{{end}}
  </span>
{{end}}`))

// reportStatus outputs the status in HTML for the web GUI.
func (r *HTTPReceiver) reportStatus(w http.ResponseWriter, req *http.Request) {
	// JSON generation copied from (go/src/expvar/expvar).expvarHandler
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "{\n")
	first := true
	expvar.Do(func(kv expvar.KeyValue) {
		if !first {
			fmt.Fprintf(&buf, ",\n")
		}
		first = false
		fmt.Fprintf(&buf, "%q: %s", kv.Key, kv.Value)
	})
	fmt.Fprintf(&buf, "\n}\n")

	var vars map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &vars); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "http://127.0.0.1:"+config.Datadog.GetString("GUI_port"))
	if err := statusTmpl.Execute(w, vars); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
