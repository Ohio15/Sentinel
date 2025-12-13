filepath = 'D:/Projects/Sentinel/agent/internal/updater/updater.go'

with open(filepath, 'r', encoding='utf-8') as f:
    content = f.read()

# The issue: %%%%LOG_FILE%%%% produces %%LOG_FILE%% in output
# But batch files need %LOG_FILE% for variable expansion
# So we need %%LOG_FILE%% in Go to produce %LOG_FILE% in output

# Fix all occurrences of %%%%LOG_FILE%%%% to %%LOG_FILE%%
content = content.replace('%%%%LOG_FILE%%%%', '%%LOG_FILE%%')

# Similarly fix %%%%errorlevel%%%% to %%errorlevel%%
content = content.replace('%%%%errorlevel%%%%', '%%errorlevel%%')

# And %%%%date%%%% %%%%time%%%% to %%date%% %%time%%
content = content.replace('%%%%date%%%%', '%%date%%')
content = content.replace('%%%%time%%%%', '%%time%%')

with open(filepath, 'w', encoding='utf-8') as f:
    f.write(content)

print('Fixed batch escape sequences')
