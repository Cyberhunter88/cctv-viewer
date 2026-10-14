import {defineConfig} from '@playwright/test';
export default defineConfig({
 testDir:'./tests',timeout:60000,workers:1,retries:0,
 use:{baseURL:'http://127.0.0.1:18080',headless:true,viewport:{width:1440,height:1000},trace:'retain-on-failure'},
 webServer:{command:'python3 scripts/ui_server.py',url:'http://127.0.0.1:18080/healthz',reuseExistingServer:false,timeout:30000},
 reporter:'list',
});
