"""Disposable server with synthetic camera recordings for browser regression tests."""
import os
import pathlib
import signal
import sqlite3
import subprocess
import tempfile
import time

with tempfile.TemporaryDirectory(prefix='cctv-ui-') as directory:
    root = pathlib.Path(directory)
    env = dict(os.environ, CTV_DEV_HTTP='true', CTV_BASE_URL='http://127.0.0.1:18080',
               CTV_ADDRESS='127.0.0.1:18080', CTV_DATA_DIR=str(root/'data'),
               CTV_RECORDINGS_DIR=str(root/'recordings'), CTV_OIDC_ISSUER='',
               CTV_ADMIN_PASSWORD='synthetic browser test password')
    subprocess.run(['bin/cctv','admin','--username','browser'],env=env,check=True)
    connection = sqlite3.connect(root/'data/viewer.db')
    for name, start in [('Eingang','08-30-00'), ('Garten','08-30-05')]:
        folder = root/'recordings'/name/'2026/10/14'
        folder.mkdir(parents=True)
        file = folder/(start+'.mp4')
        subprocess.run(['ffmpeg','-nostdin','-v','error','-f','lavfi','-i',
                        'testsrc2=size=320x180:rate=15','-t','30','-c:v','libx264',
                        '-threads','1','-pix_fmt','yuv420p','-y',str(file)],check=True)
        os.utime(file,(time.time()-120,time.time()-120))
        connection.execute('INSERT INTO cameras(id,name,folder,pattern,timezone,offset) VALUES(?,?,?,?,?,?)',
                           (name,name,name,'{YYYY}/{MM}/{DD}/{hh}-{mm}-{ss}.mp4','Europe/Berlin',0))
    reolink = root/'recordings/Reolink/Eingang/2026/10/14'
    reolink.mkdir(parents=True)
    for stamp in ['061217', '061220']:
        video = reolink/('Eingang_00_20261014'+stamp+'.mp4')
        subprocess.run(['ffmpeg','-nostdin','-v','error','-f','lavfi','-i',
                        'testsrc2=size=160x90:rate=10','-t','3','-c:v','libx264',
                        '-threads','1','-pix_fmt','yuv420p','-y',str(video)],check=True)
        image = video.with_suffix('.jpg')
        subprocess.run(['ffmpeg','-nostdin','-v','error','-i',str(video),
                        '-frames:v','1','-threads','1','-y',str(image)],check=True)
        for file in [video,image]:
            os.utime(file,(time.time()-120,time.time()-120))
    connection.commit()
    connection.close()
    server = subprocess.Popen(['bin/cctv'],env=env)
    def stop(*_):
        server.terminate()
    signal.signal(signal.SIGTERM,stop)
    signal.signal(signal.SIGINT,stop)
    raise SystemExit(server.wait())
