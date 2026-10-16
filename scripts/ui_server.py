"""Disposable server with synthetic camera recordings for browser regression tests."""
import os
import datetime
import shutil
import pathlib
import signal
import sqlite3
import subprocess
import tempfile
import time

with tempfile.TemporaryDirectory(prefix='cctv-ui-') as directory:
    root = pathlib.Path(directory)
    test_address = '127.0.0.1:'+os.environ.get('CTV_TEST_PORT','18080')
    env = dict(os.environ, CTV_DEV_HTTP='true', CTV_BASE_URL='http://'+test_address,
               CTV_ADDRESS=test_address, CTV_TRUSTED_PROXIES='127.0.0.1/32', CTV_DATA_DIR=str(root/'data'),
               CTV_RECORDINGS_DIR=str(root/'recordings'), CTV_OIDC_ISSUER='',
               CTV_ADMIN_PASSWORD='synthetic browser test password')
    subprocess.run(['bin/cctv','admin','--username','browser'],env=env,check=True)
    connection = sqlite3.connect(root/'data/viewer.db')
    for name, start in [('Testkamera-A','08-30-00'), ('Testkamera-B','08-30-05')]:
        folder = root/'recordings'/name/'2026/10/14'
        folder.mkdir(parents=True)
        file = folder/(start+'.mp4')
        subprocess.run(['ffmpeg','-nostdin','-v','error','-f','lavfi','-i',
                        'testsrc2=size=320x180:rate=15','-t','30','-c:v','libx264',
                        '-threads','1','-pix_fmt','yuv420p','-y',str(file)],check=True)
        os.utime(file,(time.time()-120,time.time()-120))
        connection.execute('INSERT INTO cameras(id,name,folder,pattern,timezone,offset) VALUES(?,?,?,?,?,?)',
                           (name,name,name,'{YYYY}/{MM}/{DD}/{hh}-{mm}-{ss}.mp4','Europe/Berlin',0))
    reolink = root/'recordings/Reolink/Testkamera-A/2026/10/14'
    reolink.mkdir(parents=True)
    for stamp in ['061217', '061220']:
        video = reolink/('Testkamera-A_00_20261014'+stamp+'.mp4' if stamp == '061217' else 'Andere Testkamera_01_20261014'+stamp+'.MP4')
        subprocess.run(['ffmpeg','-nostdin','-v','error','-f','lavfi','-i',
                        'testsrc2=size=160x90:rate=10','-t','3','-c:v','libx264',
                        '-threads','1','-pix_fmt','yuv420p','-y',str(video)],check=True)
        image = video.with_suffix('.jpg' if stamp == '061217' else '.JPEG')
        subprocess.run(['ffmpeg','-nostdin','-v','error','-i',str(video),
                        '-frames:v','1','-threads','1','-y',str(image)],check=True)
        for file in [video,image]:
            os.utime(file,(time.time()-120,time.time()-120))
    many = root/'recordings/Reolink/Bilder/2026/10/14'
    many.mkdir(parents=True)
    for i in range(105):
        stamp = (datetime.datetime(2026,10,14,7,0)+datetime.timedelta(seconds=i)).strftime('%Y%m%d%H%M%S')
        file = many/('Bilder_00_'+stamp+'.jpg')
        shutil.copyfile(reolink/'Testkamera-A_00_20261014061217.jpg',file)
        os.utime(file,(time.time()-120,time.time()-120))
    for day,name in [('2026/10/14','23-59-57'),('2026/10/15','00-00-00'),('2026/10/17','23-59-57'),('2026/10/18','00-00-00')]:
        file = root/'recordings/Mitternacht'/day/(name+'.mp4')
        file.parent.mkdir(parents=True,exist_ok=True)
        shutil.copyfile(reolink/'Testkamera-A_00_20261014061217.mp4',file)
        os.utime(file,(time.time()-120,time.time()-120))
    audio = root/'recordings/Audio/Testkamera-A/2026/10/14/08-30-00.mp4'
    audio.parent.mkdir(parents=True,exist_ok=True)
    subprocess.run(['ffmpeg','-nostdin','-v','error','-f','lavfi','-i',
                    'testsrc2=size=160x90:rate=10','-f','lavfi','-i',
                    'sine=frequency=880:sample_rate=44100','-t','30',
                    '-c:v','libx264','-threads','1','-c:a','aac','-y',str(audio)],check=True)
    os.utime(audio,(time.time()-120,time.time()-120))
    second = root/'recordings/Audio/Testkamera-B/2026/10/14/08-30-00.mp4'
    second.parent.mkdir(parents=True,exist_ok=True)
    shutil.copyfile(audio,second)
    os.utime(second,(time.time()-120,time.time()-120))
    next_audio = audio.with_name('08-30-30.mp4')
    shutil.copyfile(audio,next_audio)
    os.utime(next_audio,(time.time()-120,time.time()-120))
    for view in ['wide','zoom']:
        dual_audio = root/'recordings/AudioDual'/('CAM_20261014083000_'+view+'.mp4')
        dual_audio.parent.mkdir(parents=True,exist_ok=True)
        shutil.copyfile(audio,dual_audio)
        os.utime(dual_audio,(time.time()-120,time.time()-120))
    for camera in ['DualTestA','DualTestB']:
        folder=root/'recordings'/camera/'2026/10/14'
        folder.mkdir(parents=True)
        for stamp,view in [('083000','wide'),('083005','zoom')]:
            file=folder/('CAM_'+stamp+'_20261014'+stamp+'_'+view+'.mp4')
            shutil.copyfile(root/'recordings/Testkamera-A/2026/10/14/08-30-00.mp4',file)
            os.utime(file,(time.time()-120,time.time()-120))
    # Distinct perspectives and staggered starts catch accidental zoom-first playback.
    folder = root/'recordings/WideStart/2026/10/14'
    folder.mkdir(parents=True)
    zoom = root/'zoom-start.mp4'
    subprocess.run(['ffmpeg','-nostdin','-v','error','-f','lavfi','-i',
                    'color=c=green:size=160x90:rate=15','-t','30','-c:v','libx264',
                    '-threads','1','-pix_fmt','yuv420p','-y',str(zoom)],check=True)
    for stamp, view in [('083000','wide'),('083005','zoom'),('083100','wide'),('083105','zoom')]:
        file = folder/('CAM_20261014'+stamp+'_'+view+'.mp4')
        shutil.copyfile(zoom if view=='zoom' else root/'recordings/Testkamera-A/2026/10/14/08-30-00.mp4',file)
        os.utime(file,(time.time()-120,time.time()-120))
    # Unregistered fixtures for six-camera layouts and native aspect ratios.
    for index, dimensions in enumerate(['320x180', '240x180', '180x320']):
        template = root/('aspect-'+str(index)+'.mp4')
        subprocess.run(['ffmpeg','-nostdin','-v','error','-f','lavfi','-i',
                        'testsrc2=size='+dimensions+':rate=15','-t','30','-c:v','libx264',
                        '-threads','1','-pix_fmt','yuv420p','-y',str(template)],check=True)
        for camera in range(index,6,3):
            for kind in ['Single','Dual']:
                folder=root/'recordings'/('Six'+kind+str(camera+1))/'2026/10/14'
                folder.mkdir(parents=True)
                for view in (['wide','zoom'] if kind=='Dual' else ['single']):
                    file=folder/('CAM_20261014083000_'+view+'.mp4')
                    shutil.copyfile(template,file)
                    os.utime(file,(time.time()-120,time.time()-120))
    connection.commit()
    connection.close()
    server = subprocess.Popen(['bin/cctv'],env=env)
    def stop(*_):
        server.terminate()
    signal.signal(signal.SIGTERM,stop)
    signal.signal(signal.SIGINT,stop)
    raise SystemExit(server.wait())
