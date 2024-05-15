"""
Tool for metrics files visualization.
Expects metrics files in format <node nickname>.<date>_<time>.metrics like Primary.20230804_182932.metrics
Works with metrics collected by heapWatch.py.

Example usage for local net:
python3 ./test/heapwatch/heapWatch.py --period 10 --metrics --blockinfo --runtime 20m -o nodedata ~/networks/mylocalnet/Primary
python3 ./test/heapwatch/metrics_viz.py -d nodedata algod_transaction_messages_handled algod_tx_pool_count algod_transaction_messages_backlog_size algod_go_memory_classes_total_bytes

Also works with bdevscripts for cluster tests since it uses heapWatch.py for metrics collection.
"""

import argparse
from datetime import datetime
from enum import Enum
import glob
import logging
import os
import re
import time
from typing import Dict, Iterable, List, Union
import sys
from urllib.parse import urlparse

import dash
from dash import dcc, html
import plotly.graph_objs as go
from plotly.subplots import make_subplots

from metrics_delta import metric_line_re, num, terraform_inventory_ip_not_names
from client_ram_report import dapp

logger = logging.getLogger(__name__)

metrics_fname_re = re.compile(r'(.*?)\.(\d+_\d+)\.metrics')

def gather_metrics_files_by_nick(metrics_files: Iterable[str]) -> Dict[str, Dict[datetime, str]]:
    """return {"node nickname": {datetime: path, ...}, ...}}"""
    filesByNick = {}
    tf_inventory_path = None
    for path in metrics_files:
        fname = os.path.basename(path)
        if fname == 'terraform-inventory.host':
            tf_inventory_path = path
            continue
        m = metrics_fname_re.match(fname)
        if not m:
            continue
        nick = m.group(1)
        timestamp = m.group(2)
        timestamp = datetime.strptime(timestamp, '%Y%m%d_%H%M%S')
        dapp(filesByNick, nick, timestamp, path)
    return tf_inventory_path, filesByNick


class MetricType(Enum):
    GAUGE = 0
    COUNTER = 1

class Metric:
    """Metric with tags"""
    def __init__(self, metric_name: str, type: MetricType, value: Union[int, float]):
        full_name = metric_name.strip()
        self.name = full_name
        self.value = value
        self.type = type
        self.tags: Dict[str, str] = {}

        det_idx = self.name.find('{')
        if det_idx != -1:
            self.name = self.name[:det_idx]
            # ensure that the last character is '}'
            idx = full_name.index('}')
            if idx != len(full_name) - 1:
                raise ValueError(f'Invalid metric name: {full_name}')
            raw_tags = full_name[full_name.find('{')+1:full_name.find('}')]
            tags = raw_tags.split(',')
            for tag in tags:
                key, value = tag.split('=')
                self.tags[key] = value

    def short_name(self):
        return self.name

    def __str__(self):
        return self.string()

    def string(self):
        result = self.name
        if self.tags:
            result += '{' + ','.join([f'{k}={v}' for k, v in sorted(self.tags.items())]) + '}'
        return result

    def add_tag(self, key: str, value: str):
        self.tags[key] = value


def parse_metrics(
    fin: Iterable[str], nick: str, metrics_names: set=None, diff: bool=None
) -> Dict[str, List[Metric]]:
    """Parse metrics file and return dicts of values and types"""
    out = {}
    try:
        last_type = None
        for line in fin:
            if not line:
                continue
            line = line.strip()
            if not line:
                continue
            if line[0] == '#':
                if line.startswith('# TYPE'):
                    tpe = line.split()[-1]
                    if tpe == 'gauge':
                        last_type = MetricType.GAUGE
                    elif tpe == 'counter':
                        last_type = MetricType.COUNTER
                continue
            m = metric_line_re.match(line)
            if m:
                name = m.group(1)
                value = num(m.group(2))
            else:
                ab = line.split()
                name = ab[0]
                value = num(ab[1])

            metric = Metric(name, last_type, value)
            metric.add_tag('n', nick)
            if not metrics_names or metric.name in metrics_names:
                if metric.name not in out:
                    out[metric.name] = [metric]
                else:
                    out[metric.name].append(metric)
    except:
        print(f'An exception occurred in parse_metrics: {sys.exc_info()}')
        pass
    if diff and metrics_names and len(metrics_names) == 2 and len(out) == 2:
        m = list(out.keys())
        name = f'{m[0]}_-_{m[1]}'
        metric = Metric(name, MetricType.GAUGE, out[m[0]].value - out[m[1]].value)
        out = [{name: metric}]

    return out


def main():
    os.environ['TZ'] = 'UTC'
    time.tzset()
    default_img_filename = 'metrics_viz.png'
    default_html_filename = 'metrics_viz.html'

    ap = argparse.ArgumentParser()
    ap.add_argument('metrics_names', nargs='+', default=None, help='metric name(s) to track')
    ap.add_argument('-d', '--dir', type=str, default=None, help='dir path to find /*.metrics in')
    ap.add_argument('-l', '--list-nodes', default=False, action='store_true', help='list available node names with metrics')
    ap.add_argument('--nick-re', action='append', default=[], help='regexp to filter node names, may be repeated')
    ap.add_argument('--nick-lre', action='append', default=[], help='label:regexp to filter node names, may be repeated')
    ap.add_argument('-s', '--save', type=str, choices=['png', 'html'], help=f'save plot to \'{default_img_filename}\' or \'{default_html_filename}\' file instead of showing it')
    ap.add_argument('--diff', action='store_true', default=None, help='diff two gauge metrics instead of plotting their values. Requires two metrics names to be set')
    ap.add_argument('--verbose', default=False, action='store_true')

    args = ap.parse_args()
    if args.verbose:
        logging.basicConfig(level=logging.DEBUG)
    else:
        logging.basicConfig(level=logging.INFO)

    if not args.dir:
        logging.error('need at least one dir set with -d/--dir')
        return 1

    metrics_files = sorted(glob.glob(os.path.join(args.dir, '*.metrics')))
    metrics_files.extend(glob.glob(os.path.join(args.dir, 'terraform-inventory.host')))
    tf_inventory_path, filesByNick = gather_metrics_files_by_nick(metrics_files)
    if tf_inventory_path:
        # remap ip addresses to node names
        ip_to_name = terraform_inventory_ip_not_names(tf_inventory_path)
        filesByNick2 = {}
        for nick in filesByNick.keys():
            parsed = urlparse('//' + nick)
            name: str = ip_to_name.get(parsed.hostname)
            val = filesByNick[nick]
            filesByNick2[name] = val

        filesByNick = filesByNick2
        filesByNick2 = {}

        for nick in filesByNick.keys():
            if args.nick_re or not args.nick_re and not args.nick_lre:
                # filter by regexp or apply default renaming
                for nick_re in args.nick_re:
                    if re.match(nick_re, nick):
                        break
                else:
                    if args.nick_re:
                        # regex is given but not matched, continue to the next node
                        continue

                # apply default renaming
                name = nick
                idx = name.find('_')
                if idx != -1:
                    name = name[idx+1:]
                val = filesByNick[nick]
                filesByNick2[name] = val

            elif args.nick_lre:
                # filter by label:regexp
                label = None
                for nick_lre in args.nick_lre:
                    label, nick_re = nick_lre.split(':')
                    if re.match(nick_re, nick):
                        break
                else:
                    if args.nick_lre:
                        # regex is given but not matched, continue to the next node
                        continue

                val = filesByNick[nick]
                filesByNick2[label] = val
            else:
                raise RuntimeError('unexpected options combination')

        if filesByNick2:
            filesByNick = filesByNick2

    if args.list_nodes:
        print('Available nodes:', ', '.join(sorted(filesByNick.keys())))
        return 0

    app = dash.Dash(__name__)
    app.layout = html.Div(
        html.Div([
            html.H4('Algod Metrics'),
            html.Div(id='text'),
            dcc.Graph(id='graph'),
        ])
    )
    metrics_names = set(args.metrics_names)
    nrows = 1 if args.diff and len(args.metrics_names) == 2 else len(metrics_names)

    fig = make_subplots(
        rows=nrows, cols=1,
        vertical_spacing=0.03, shared_xaxes=True,
        subplot_titles=[f'{name}' for name in sorted(metrics_names)],
    )

    fig['layout']['margin'] = {
        'l': 30, 'r': 10, 'b': 10, 't': 20
    }
    fig['layout']['height'] = 500 * nrows
    # fig.update_layout(template="plotly_dark")

    data = {
        'time': [],
    }
    raw_series = {}
    for nick, items in filesByNick.items():
        active_metrics = {}
        for dt, metrics_file in items.items():
            data['time'].append(dt)
            with open(metrics_file, 'rt') as f:
                metrics = parse_metrics(f, nick, metrics_names, args.diff)
                for metric_name, metrics_seq in metrics.items():
                    active_metric_names = []
                    for metric in metrics_seq:
                        raw_value = metric.value
                        full_name = metric.string()
                        if full_name not in data:
                            data[full_name] = []
                            raw_series[full_name] = []
                        metric_value = metric.value
                        if metric.type == MetricType.COUNTER:
                            if len(raw_series[full_name]) > 0:
                                metric_value = (metric_value - raw_series[full_name][-1]) / (dt - data['time'][-2]).total_seconds()
                            else:
                                metric_value = 0

                        data[full_name].append(metric_value)
                        raw_series[full_name].append(raw_value)

                        active_metric_names.append(full_name)

                    active_metric_names.sort()
                    active_metrics[metric_name] = active_metric_names

        for i, metric_pair in enumerate(sorted(active_metrics.items())):
            metric_name, metric_fullnames = metric_pair
            for metric_fullname in metric_fullnames:
                fig.append_trace(go.Scatter(
                    x=data['time'],
                    y=data[metric_fullname],
                    name=metric_fullname,
                    mode='lines+markers',
                    line=dict(width=1),
                ), i+1, 1)

    if args.save:
        if args.save == 'html':
            target_path = os.path.join(args.dir, default_html_filename)
            fig.write_html(target_path)
        else:
            target_path = os.path.join(args.dir, default_img_filename)
            fig.write_image(target_path)
        print(f'Saved plot to {target_path}')
    else:
        fig.show()

    # app.run_server(debug=True)
    return 0

if __name__ == '__main__':
    sys.exit(main())