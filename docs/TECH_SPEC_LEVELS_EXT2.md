## 1. Additional chart feature 

Add a new chart(linear) to existing chart that shows our values from conclusion indicator, I think we have to make conclusion on the backend  

## 2. Automation of level setting

Add a new feature that allows to set levels after some level fails (after few fails I mean close position by base level ), for example if we have 5 fails to close level we set new level above or below last level with some offset, or we can set new level on the edge of book order lines that have the most volume  then current level is destroyed. And if level works in this mode we delete all previous levels that were set by auto mode. This mode can be enabled by checkbox in settings. Acually we can set this mode by default and disable it only by checkbox. Also it data should be saved in database, I mean add flags to level model that shows if level was set by auto mode or not.
