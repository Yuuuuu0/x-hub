"""生成演示环境的预置数据 web/src/demo/fixtures.json 与静态分享页 web/src/demo/public/share/。

向一个全新的本地主控写入演示数据（服务器、入站、订阅、外部节点、分流方案），
抓取各接口的真实响应，再补上被控端在线时才有的运行数据（心跳、网速、用量等）。
各订阅的分享页及其各格式内容同样由主控渲染，保存为静态文件，演示环境中的「预览分享页」打开它们。

用法：
  make master
  ./dist/xhub-master settings proxy --data /tmp/xhub-demo --public-url https://hub.example.com --trusted 127.0.0.1/32
  ./dist/xhub-master --data /tmp/xhub-demo        # 日志中打印面板路径与初始密码
  python3 web/src/demo/fixtures.py http://127.0.0.1:2096/<面板路径>/api/ <初始密码> /tmp/xhub-demo
"""
import html, json, os, shutil, sqlite3, sys, time, urllib.error, urllib.parse, urllib.request

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), '../../..'))
BASE = sys.argv[1]
PW = sys.argv[2]
DATA_DIR = sys.argv[3]
op = urllib.request.build_opener()
COOKIE = {}


def call(m, p, body=None):
    """以管理员身份调用面板接口；会话 Cookie 带 Secure 标记，这里手动保存。"""
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(BASE + p, data=data, method=m, headers={
        'Content-Type': 'application/json', 'X-Forwarded-Proto': 'https', 'Host': 'hub.example.com',
        'Cookie': '; '.join(f'{k}={v}' for k, v in COOKIE.items())})
    try:
        r = op.open(req)
        t = r.read()
        for h in r.headers.get_all('Set-Cookie') or []:
            k, v = h.split(';')[0].split('=', 1)
            COOKIE[k] = v
        return json.loads(t) if t else None
    except urllib.error.HTTPError as e:
        print('ERR', m, p, e.code, e.read().decode())
        raise


# ---- 写入演示数据 ----
call('POST', 'login', {'username': 'admin', 'password': PW})
call('PUT', 'settings/certs', {'acmeEmail': 'admin@example.com', 'cloudflareToken': 'demo-token'})
day = 86400; now = int(time.time())
G = 1 << 30
nodes = [
  dict(name='tokyo-1', remark='', publicHosts=[dict(host='203.0.113.11', label='')], country='JP', region='', netResetDay=1, netQuotaBytes=2048*G, netCountMode='tx', expireAt=now+69*day),
  dict(name='hk-hkt', remark='', publicHosts=[dict(host='203.0.113.22', label='')], country='HK', region='', netResetDay=15, netQuotaBytes=1024*G, netCountMode='both', expireAt=now+142*day),
  dict(name='sg-1', remark='', publicHosts=[dict(host='203.0.113.33', label='')], country='SG', region='', netResetDay=1, netQuotaBytes=1024*G, netCountMode='max', expireAt=now+23*day),
  dict(name='us-lax', remark='', publicHosts=[dict(host='198.51.100.44', label='')], country='US', region='洛杉矶', netResetDay=8, netQuotaBytes=3072*G, netCountMode='tx', expireAt=now+301*day),
  dict(name='de-fra', remark='', publicHosts=[dict(host='198.51.100.55', label='')], country='DE', region='', netResetDay=1, netQuotaBytes=0, netCountMode='both', expireAt=0),
]
ids = []
for n in nodes:
    ids.append(call('POST', 'nodes', n)['id'])
def reality(sni):
    k = call('POST', 'tools/x25519', {})
    sid = call('GET', 'tools/short-id?bytes=8')['shortId']
    return {'target': sni + ':443', 'serverNames': [sni], 'privateKey': k['privateKey'], 'publicKey': k['publicKey'], 'shortIds': [sid], 'fingerprint': 'chrome', 'spiderX': '/'}
def tls(name):
    return {'serverName': name, 'certMode': 'acme', 'certFile': '', 'keyFile': '', 'alpn': ['h2', 'http/1.1'], 'fingerprint': 'chrome'}
ins = [
  (ids[0], 'acck', 443, 'vless', {'network': 'tcp', 'security': 'reality', 'vless': {'flow': 'xtls-rprx-vision'}, 'reality': reality('www.microsoft.com'), 'sniffing': True}),
  (ids[0], 'xhttp', 8443, 'vless', {'network': 'xhttp', 'security': 'reality', 'reality': reality('www.apple.com'), 'xhttp': {'path': '/xh', 'host': '', 'mode': 'auto'}, 'sniffing': True}),
  (ids[1], 'hkt', 443, 'vless', {'network': 'tcp', 'security': 'reality', 'vless': {'flow': 'xtls-rprx-vision'}, 'reality': reality('www.cloudflare.com'), 'sniffing': True}),
  (ids[1], 'cdn', 2083, 'vless', {'network': 'ws', 'security': 'tls', 'ws': {'path': '/ws', 'host': ''}, 'tls': tls('hk.example.com'), 'sniffing': True}),
  (ids[2], 'sg', 443, 'vless', {'network': 'tcp', 'security': 'reality', 'vless': {'flow': 'xtls-rprx-vision'}, 'reality': reality('www.amazon.com'), 'sniffing': True}),
  (ids[2], 'trojan', 8443, 'trojan', {'network': 'tcp', 'security': 'tls', 'tls': tls('sg.example.com'), 'sniffing': True}),
  (ids[3], 'cn2', 443, 'vless', {'network': 'tcp', 'security': 'reality', 'vless': {'flow': 'xtls-rprx-vision'}, 'reality': reality('www.nvidia.com'), 'sniffing': True}),
  (ids[4], 'relay', 23456, 'shadowsocks', {'network': 'tcp', 'security': 'none', 'ss': {'method': '2022-blake3-aes-128-gcm', 'serverKey': call('GET', 'tools/ss-key?method=2022-blake3-aes-128-gcm')['key']}, 'sniffing': True}),
]
iids = []
for nid, rm, port, proto, spec in ins:
    iids.append(call('POST', 'inbounds', {'nodeId': nid, 'remark': rm, 'enabled': True, 'listen': '', 'port': port, 'protocol': proto, 'spec': spec, 'linkAddrs': []})['id'])
subs = call('GET', 'subscriptions')['subscriptions']
call('PUT', f"subscriptions/{subs[0]['id']}", {'name': subs[0]['name'], 'note': '自用全部节点', 'enabled': True})
for name, note, allin, inb, lim, exp, rd in [
  ('爸妈', '手机 + 电视', False, [iids[2], iids[0]], 100*G, now+200*day, 1),
  ('Alice', '', True, [iids[7]], 300*G, now+95*day, 10),
  ('Bob', '室友', False, [iids[0], iids[4]], 50*G, now+12*day, 1),
  ('工作电脑', '', True, [], 0, 0, 0),
]:
    s = call('POST', 'subscriptions', {'name': name, 'note': note, 'enabled': True, 'allInbounds': allin, 'inboundIds': inb, 'limitBytes': lim, 'expireAt': exp, 'resetDay': rd})

allsubs = [x['id'] for x in call('GET', 'subscriptions')['subscriptions']]
call('POST', 'external', {'kind': 'manual', 'name': '住宅 IP', 'enabled': True, 'content': 'socks5://user:pass@192.0.2.10:1080#🇺🇸 美国家宽', 'userAgent': '', 'interval': 0, 'prefix': '', 'include': '', 'exclude': '', 'chain': f'inbound:{iids[6]}', 'allSubs': False, 'subIds': [allsubs[0]]})
links = "\n".join([
 'hysteria2://pw@hk1.airport.example:443?sni=hk1.airport.example#🇭🇰 香港 01',
 'hysteria2://pw@hk2.airport.example:443?sni=hk2.airport.example#🇭🇰 香港 02',
 'trojan://pw@tw1.airport.example:443?sni=tw1.airport.example#🇹🇼 台湾 01',
 'trojan://pw@jp1.airport.example:443?sni=jp1.airport.example#🇯🇵 日本 01',
 'trojan://pw@us1.airport.example:443?sni=us1.airport.example#🇺🇸 美国 01',
 'trojan://pw@info.airport.example:443?sni=info.airport.example#剩余流量：312.5 GB',
])
call('POST', 'external', {'kind': 'manual', 'name': '备用机场', 'enabled': True, 'content': links, 'userAgent': '', 'interval': 0, 'prefix': '[备用] ', 'include': '', 'exclude': '剩余|到期|官网', 'chain': '', 'allSubs': False, 'subIds': [allsubs[0], allsubs[2]]})
yaml = open(os.path.join(ROOT, 'data/mihomo_template.yaml')).read()
r = call('POST', 'profiles/import', {'name': '日常分流', 'yaml': yaml})
call('POST', 'profiles', {'name': '精简', 'note': '只区分国内外'})


# ---- 抓取接口响应 ----
out = {}
for p in ['nodes', 'inbounds', 'subscriptions', 'external', 'profiles', 'profiles/defaults', 'rulesets', 'settings', 'xray/releases']:
    try: out[p] = call('GET', p)
    except Exception as e: print('skip', p)
for pr in out['profiles']['profiles']:
    out[f"profiles/{pr['id']}"] = call('GET', f"profiles/{pr['id']}")
    for c in ['mihomo', 'singbox', 'shadowrocket']:
        out[f"preview/{pr['id']}/{c}"] = call('POST', 'profiles/preview', {'subscriptionId': 1, 'client': c, 'spec': pr['spec']})
for i in out['inbounds']['inbounds']:
    out[f"inbounds/{i['id']}/links"] = call('GET', f"inbounds/{i['id']}/links")
out['install-command'] = call('POST', 'nodes/1/install-command', {})
d = out


# ---- 补充运行数据 ----
T = 1 << 40
xv = d['xray/releases']['releases'][0]['tag']
av = d['nodes']['agentVersion']
hosts = [
  ('tokyo-1', 'debian', '12.8', '6.1.0-27-amd64', 'amd64', 'AMD EPYC 7B13', 2, 4, 40),
  ('hk-hkt', 'ubuntu', '24.04', '6.8.0-48-generic', 'amd64', 'Intel Xeon Platinum 8272CL', 1, 1, 20),
  ('sg-1', 'debian', '12.8', '6.1.0-27-cloud-arm64', 'arm64', 'Neoverse-N1', 4, 24, 100),
  ('us-lax', 'debian', '12.8', '6.1.0-27-amd64', 'amd64', 'AMD Ryzen 9 7950X', 2, 2, 30),
  ('de-fra', 'ubuntu', '22.04', '5.15.0-125-generic', 'amd64', 'Intel Xeon E5-2680 v4', 1, 1, 15),
]
rt = [  # cpu, mem frac, disk frac, rx rate, tx rate, period rx, tx, uptime days
  (7.4, 0.46, 0.31, 2.1e6, 18.6e6, 52*G, 1.21*T, 41),
  (12.8, 0.62, 0.55, 0.9e6, 6.3e6, 21*G, 386*G, 18),
  (3.1, 0.18, 0.12, 0.3e6, 1.4e6, 6*G, 74*G, 63),
  (21.5, 0.71, 0.44, 4.6e6, 38.2e6, 88*G, 1.86*T, 12),
  (1.2, 0.33, 0.27, 0.05e6, 0.2e6, 1.5*G, 9*G, 97),
]
for n, h, r in zip(d['nodes']['nodes'], hosts, rt):
    name, plat, pv, kern, arch, cpu, cores, mem, disk = h
    c, mf, df, rx, tx, prx, ptx, up = r
    n.update(enrolled=True, agentVersion=av, xrayVersion=xv, caps=['x25519mlkem768'] if False else n['caps'], lastIp=n['publicHosts'][0]['host'], lastSeenAt=now,
             appliedRev=n['desiredRev'], appliedHash='demo', syncState='synced', syncError='')
    n['host'] = dict(hostname=name, os='linux', platform=plat, platformVersion=pv, kernel=kern, arch=arch, cpuModel=cpu, cpuCores=cores)
    n['geo']['detected'] = n['geo']['code']
    mode = n['netCountMode']
    used = {'both': prx + ptx, 'tx': ptx, 'max': max(prx, ptx)}[mode]
    n['netPeriod'].update(rx=int(prx), tx=int(ptx), used=int(used))
    n['runtime'] = dict(online=True, connectedAt=now - 3*86400, ip=n['publicHosts'][0]['host'], rxRate=int(rx), txRate=int(tx), caps=None,
      xray=dict(installed=True, version=xv, enabled=True, running=True, startedAt=now - 3*86400),
      heartbeat=dict(cpu=c, memUsed=int(mem*G*mf), memTotal=mem*G, swapUsed=0, swapTotal=(1*G if mem < 4 else 0), diskUsed=int(disk*G*df), diskTotal=disk*G,
        load1=round(c/100*cores*1.3, 2), load5=round(c/100*cores*1.1, 2), load15=round(c/100*cores, 2), uptime=up*86400, netRx=int(prx*3), netTx=int(ptx*3)))
# de-fra 用旧版 Xray，展示可升级
d['nodes']['nodes'][4]['xrayVersion'] = d['xray/releases']['releases'][1]['tag']
d['nodes']['nodes'][4]['runtime']['xray']['version'] = d['xray/releases']['releases'][1]['tag']
usage = {  # name: up, down, online ips
  '自己': (14*G, 486*G, ['198.51.100.7', '203.0.113.90']),
  '爸妈': (1.2*G, 37*G, ['192.0.2.41']),
  'Alice': (6*G, 128*G, ['192.0.2.88']),
  'Bob': (1.1*G, 31.5*G, []),
  '工作电脑': (0.8*G, 22*G, ['198.51.100.7']),
}
for s in d['subscriptions']['subscriptions']:
    up, down, ips = usage[s['name']]
    s.update(usedUp=int(up), usedDown=int(down), onlineIps=ips)
d['subscriptions']['subscriptions'][0]['profileId'] = 1
d['subscriptions']['subscriptions'][2]['profileId'] = 1
for e in d['external']['sources']:
    e['source']['fetchedAt'] = now - 3600
for i, r in enumerate(d['rulesets']['ruleSets']):
    r['rules'] = r.get('rules') or [1832, 412, 96, 15420, 287, 5340, 64, 1290][i % 8]
d['settings']['access']['panelPath'] = 'demo'
d['profiles']['profiles'][0]['users'] = ['自己', 'Alice']
src = d['external']['sources'][1]['source']
src.update(kind='remote', content='https://airport.example/api/v1/client/subscribe?token=demo', interval=360, userAgent='clash.meta',
  userInfo=f'upload={int(12.4*G)}; download={int(175.1*G)}; total={500*G}; expire={now + 48*86400}')
d['capturedAt'] = now
json.dump(d, open(os.path.join(ROOT, 'web/src/demo/fixtures.json'), 'w'), ensure_ascii=False, separators=(',', ':'))
print('已写入 web/src/demo/fixtures.json')


# ---- 静态分享页 ----
# 分享页按数据库中的用量渲染，先把上面补充的用量与方案写回数据库。
db = sqlite3.connect(os.path.join(DATA_DIR, 'xhub.db'))
for s in d['subscriptions']['subscriptions']:
    db.execute('UPDATE subscriptions SET used_up = ?, used_down = ?, profile_id = ? WHERE id = ?', (s['usedUp'], s['usedDown'], s['profileId'], s['id']))
db.commit()
db.close()

ORIGIN = '{0.scheme}://{0.netloc}'.format(urllib.parse.urlparse(BASE))
FORMATS = {'mihomo': '.yaml', 'singbox': '.json', 'base64': '.txt', 'shadowrocket': '.conf'}
OUT = os.path.join(ROOT, 'web/src/demo/public/share')
shutil.rmtree(OUT, ignore_errors=True)


def fetch(path):
    return urllib.request.urlopen(urllib.request.Request(ORIGIN + path, headers={'User-Agent': 'Mozilla/5.0'})).read().decode()


for s in d['subscriptions']['subscriptions']:
    path = urllib.parse.urlparse(s['url']).path  # /<订阅路径>/<token>
    out = os.path.join(OUT, s['token'])
    os.makedirs(out)
    page = fetch(path + '?format=page')
    for f, ext in FORMATS.items():
        file = f + ext
        save_as = html.escape(f'x-hub-{s["name"]}{ext}')
        # 查看页与下载改为同目录下的静态文件
        page = page.replace(f'href="{s["url"]}?format={f}&amp;view=1"', f'href="{f}.html"')
        page = page.replace(f'href="{s["url"]}?format={f}&amp;download=1" download', f'href="{file}" download="{save_as}"')
        viewer = fetch(f'{path}?format={f}&view=1')
        viewer = viewer.replace(f'href="{path}?format={f}&amp;download=1" download', f'href="{file}" download="{save_as}"')
        viewer = viewer.replace(f'href="{path}?format=page"', 'href="./"')
        open(os.path.join(out, f + '.html'), 'w').write(viewer)
        open(os.path.join(out, file), 'w').write(fetch(f'{path}?format={f}&download=1'))
    open(os.path.join(out, 'index.html'), 'w').write(page)
print('已写入 web/src/demo/public/share/')
