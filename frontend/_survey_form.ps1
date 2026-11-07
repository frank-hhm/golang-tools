$ErrorActionPreference = 'Continue'
Set-Location $PSScriptRoot\..\src
$files = Get-ChildItem -Recurse -Filter *.vue | Where-Object { $_.FullName -notmatch 'components\\common\\theme|components\\mui' }
$formAttrs=@{}; $fiAttrs=@{}; $inner=@{}; $validateApi=@{}
function Add($h, $k) { if (-not $h.ContainsKey($k)) { $h[$k] = 0 }; $h[$k]++ }
foreach ($f in $files) {
  $t = [IO.File]::ReadAllText($f.FullName)
  # a-form 开标签属性
  foreach ($m in [regex]::Matches($t, '<a-form(\s(?:[^>"]|"[^"]*")*?)?>')) {
    foreach ($a in [regex]::Matches($m.Groups[1].Value, '(?<![\w:-])([\w.-]+)(?==|[\s>]|$)')) { Add $formAttrs $a.Groups[1].Value }
  }
  # a-form-item 开标签属性
  foreach ($m in [regex]::Matches($t, '<a-form-item(\s(?:[^>"]|"[^"]*")*?)?>')) {
    foreach ($a in [regex]::Matches($m.Groups[1].Value, '(?<![\w:-])([\w.-]+)(?==|[\s>]|$)')) { Add $fiAttrs $a.Groups[1].Value }
  }
  # form-item 内第一个组件标签
  foreach ($m in [regex]::Matches($t, '<a-form-item[^>]*>\s*<([a-z]+-[a-z-]+|m-[a-z-]+|[A-Za-z]+)', 'Singleline')) { Add $inner $m.Groups[1].Value }
  # 校验调用面
  foreach ($m in [regex]::Matches($t, '\.(validate|validateField|clearValidate|resetFields|scrollToField)\(')) { Add $validateApi $m.Groups[1].Value }
}
"== a-form attrs:"; $formAttrs.GetEnumerator() | Sort-Object Value -Descending | ForEach-Object { "{0,4} {1}" -f $_.Value, $_.Key }
"== a-form-item attrs:"; $fiAttrs.GetEnumerator() | Sort-Object Value -Descending | ForEach-Object { "{0,4} {1}" -f $_.Value, $_.Key }
"== first control inside form-item:"; $inner.GetEnumerator() | Sort-Object Value -Descending | ForEach-Object { "{0,4} {1}" -f $_.Value, $_.Key }
"== form api calls:"; $validateApi.GetEnumerator() | Sort-Object Value -Descending | ForEach-Object { "{0,4} {1}" -f $_.Value, $_.Key }
